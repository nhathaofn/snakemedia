package muse

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"SnakeMedia/internal/media"
	"SnakeMedia/pkg/utils"
)

// MuseAssetType identifies the category prefix in the Muse asset naming convention
// ([PREFIX_]TYPE_alias[_angle][_variant][_state][_vN].ext).
type MuseAssetType string

const (
	MuseTypeChar  MuseAssetType = "CHAR"
	MuseTypeAnim  MuseAssetType = "ANIM"
	MuseTypeLoc   MuseAssetType = "LOC"
	MuseTypeProp  MuseAssetType = "PROP"
	MuseTypeVeh   MuseAssetType = "VEH"
	MuseTypeDoc   MuseAssetType = "DOC"
	MuseTypeStyle MuseAssetType = "STYLE"
)

var AllMuseAssetTypes = []struct {
	Value  MuseAssetType
	TSName string
}{
	{MuseTypeChar, "Char"},
	{MuseTypeAnim, "Anim"},
	{MuseTypeLoc, "Loc"},
	{MuseTypeProp, "Prop"},
	{MuseTypeVeh, "Veh"},
	{MuseTypeDoc, "Doc"},
	{MuseTypeStyle, "Style"},
}

func (t MuseAssetType) valid() bool {
	return slices.ContainsFunc(AllMuseAssetTypes, func(x struct {
		Value  MuseAssetType
		TSName string
	}) bool {
		return x.Value == t
	})
}

const ungroupedProjectFolder = "_chung"

// ParsedAssetName holds fields extracted from a convention-compliant filename.
type ParsedAssetName struct {
	Project  string        `json:"project,omitempty"`
	Type     MuseAssetType `json:"type"`
	Role     MuseRefRole   `json:"role"`
	Alias    string        `json:"alias"`
	Angle    string        `json:"angle,omitempty"`
	Variant  string        `json:"variant,omitempty"`
	State    string        `json:"state,omitempty"`
	Version  int           `json:"version,omitempty"`
	Rejected bool          `json:"rejected,omitempty"`
	Hint     string        `json:"hint,omitempty"`
}

func defaultRoleForType(t MuseAssetType) MuseRefRole {
	switch t {
	case MuseTypeChar:
		return MuseRoleCharacter
	case MuseTypeAnim:
		return MuseRoleAnimal
	case MuseTypeLoc:
		return MuseRoleSetting
	case MuseTypeProp:
		return MuseRoleProp
	case MuseTypeVeh:
		return MuseRoleVehicle
	case MuseTypeDoc:
		return MuseRoleDocument
	case MuseTypeStyle:
		return MuseRoleStyle
	default:
		return MuseRoleNone
	}
}

func typeForRole(r MuseRefRole) MuseAssetType {
	switch r {
	case MuseRoleFace, MuseRoleCharacter:
		return MuseTypeChar
	case MuseRoleAnimal:
		return MuseTypeAnim
	case MuseRoleSetting, MuseRoleStoryboard:
		return MuseTypeLoc
	case MuseRoleVehicle:
		return MuseTypeVeh
	case MuseRoleDocument:
		return MuseTypeDoc
	case MuseRoleStyle:
		return MuseTypeStyle
	default:
		return MuseTypeProp
	}
}

var (
	yearTokenRe    = regexp.MustCompile(`^(?:1\d{3}|20\d{2})$`)
	versionTokenRe = regexp.MustCompile(`^v([1-9]\d*)$`)
)

var charAngleTokens = map[string]bool{
	"front": true, "close": true, "34": true, "side": true, "back": true, "full": true, "phone": true,
}

var locAreaTokens = map[string]bool{
	"wide": true, "table": true, "door": true, "window": true,
}

var variantTokens = map[string]bool{
	"young": true, "old": true, "work": true, "dress": true, "uniform": true,
}

var stateTokens = map[string]bool{
	"wet": true, "soot": true, "injured": true, "crying": true,
	"dawn": true, "day": true, "dusk": true, "night": true,
	"before": true, "burning": true, "after": true, "storm": true, "rain": true,
	"full": true, "empty": true, "text": true,
}

// normalizeProject turns a project prefix into uppercase ASCII alphanumeric/underscore
// (e.g. "Oliva Cottage" -> "OLIVA_COTTAGE", "oliva" -> "OLIVA").
func normalizeProject(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, ungroupedProjectFolder) {
		return ""
	}
	s := slugifyToken(trimmed, false)
	if s == "" {
		return ""
	}
	return strings.ToUpper(s)
}

// slugifyToken strips Vietnamese accents, lowercases, and replaces separators with '_'.
// When allowPlus is true, '+' is preserved for group asset tags such as "jimena+hester".
func slugifyToken(raw string, allowPlus bool) string {
	var b strings.Builder
	prevSep := true
	for _, r := range strings.TrimSpace(raw) {
		r = foldVietnameseRune(unicode.ToLower(r))
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevSep = false
		case allowPlus && r == '+':
			if b.Len() > 0 && !prevSep {
				b.WriteByte('+')
				prevSep = true
			}
		case r == '_' || r == '-' || unicode.IsSpace(r):
			if b.Len() > 0 && !prevSep {
				b.WriteByte('_')
				prevSep = true
			}
		}
	}
	out := strings.Trim(b.String(), "_+")
	return out
}

// normalizeTagToken normalizes an @tag keeping Unicode letters/digits/_/+/-.
func normalizeTagToken(raw string) string {
	s := strings.TrimPrefix(strings.TrimSpace(raw), "@")
	var b strings.Builder
	prevSep := true
	for _, r := range s {
		r = foldVietnameseRune(unicode.ToLower(r))
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevSep = false
		case r == '+' || r == '_' || r == '-':
			if b.Len() > 0 && !prevSep {
				b.WriteRune(r)
				prevSep = true
			}
		case unicode.IsSpace(r):
			if b.Len() > 0 && !prevSep {
				b.WriteByte('_')
				prevSep = true
			}
		}
	}
	return strings.Trim(b.String(), "_-+")
}

func foldVietnameseRune(r rune) rune {
	switch r {
	case 'đ':
		return 'd'
	case 'á', 'à', 'ả', 'ã', 'ạ', 'ă', 'ắ', 'ằ', 'ẳ', 'ẵ', 'ặ', 'â', 'ấ', 'ầ', 'ẩ', 'ẫ', 'ậ':
		return 'a'
	case 'é', 'è', 'ẻ', 'ẽ', 'ẹ', 'ê', 'ế', 'ề', 'ể', 'ễ', 'ệ':
		return 'e'
	case 'í', 'ì', 'ỉ', 'ĩ', 'ị':
		return 'i'
	case 'ó', 'ò', 'ỏ', 'õ', 'ọ', 'ô', 'ố', 'ồ', 'ổ', 'ỗ', 'ộ', 'ơ', 'ớ', 'ờ', 'ở', 'ỡ', 'ợ':
		return 'o'
	case 'ú', 'ù', 'ủ', 'ũ', 'ụ', 'ư', 'ứ', 'ừ', 'ử', 'ữ', 'ự':
		return 'u'
	case 'ý', 'ỳ', 'ỷ', 'ỹ', 'ỵ':
		return 'y'
	default:
		return r
	}
}

// BuildAssetFileName formats the canonical convention filename:
// [PREFIX_]LOẠI_alias[_góc][_biến_thể][_trạng_thái][_vN][_rej].ext
func BuildAssetFileName(project string, assetType MuseAssetType, alias, angle, variant, state string, version int, rejected bool, ext string) string {
	proj := normalizeProject(project)
	if !assetType.valid() {
		assetType = MuseTypeProp
	}
	aliasSlug := slugifyToken(alias, true)
	if aliasSlug == "" {
		aliasSlug = "asset"
	}
	parts := make([]string, 0, 7)
	if proj != "" {
		parts = append(parts, proj)
	}
	parts = append(parts, string(assetType), aliasSlug)
	if a := slugifyToken(angle, false); a != "" {
		parts = append(parts, a)
	}
	if v := slugifyToken(variant, false); v != "" {
		parts = append(parts, v)
	}
	if s := slugifyToken(state, false); s != "" {
		parts = append(parts, s)
	}
	if version >= 2 {
		parts = append(parts, fmt.Sprintf("v%d", version))
	}
	if rejected {
		parts = append(parts, "rej")
	}
	cleanExt := strings.ToLower(strings.TrimSpace(ext))
	if cleanExt == "" {
		cleanExt = ".png"
	}
	if !strings.HasPrefix(cleanExt, ".") {
		cleanExt = "." + cleanExt
	}
	return strings.Join(parts, "_") + cleanExt
}

// VaultRelPath computes the relative path inside VaultDir using '/' separators.
func VaultRelPath(project, fileName string, rejected bool) string {
	folder := normalizeProject(project)
	if folder == "" {
		folder = ungroupedProjectFolder
	}
	if rejected {
		return folder + "/rejected/" + fileName
	}
	return folder + "/" + fileName
}

// ParseAssetFileName extracts project, type, alias, angle, variant, state,
// version and rejected flags from a filename if it matches the Muse convention.
func ParseAssetFileName(fileName string) *ParsedAssetName {
	stem := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	if stem == "" {
		return nil
	}
	tokens := strings.Split(stem, "_")
	if len(tokens) < 2 {
		return nil
	}

	rejected := false
	if strings.EqualFold(tokens[len(tokens)-1], "rej") && len(tokens) > 2 {
		rejected = true
		tokens = tokens[:len(tokens)-1]
	}
	version := 0
	if len(tokens) > 2 {
		if m := versionTokenRe.FindStringSubmatch(strings.ToLower(tokens[len(tokens)-1])); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 {
				version = n
				tokens = tokens[:len(tokens)-1]
			}
		}
	}

	var project string
	var assetType MuseAssetType
	var role MuseRefRole
	var hint string
	aliasStart := -1

	parseTypeToken := func(tok string) (MuseAssetType, MuseRefRole, string, bool) {
		up := strings.ToUpper(tok)
		if t := MuseAssetType(up); t.valid() {
			return t, defaultRoleForType(t), "", true
		}
		switch up {
		case "FF", "LF":
			return MuseTypeStyle, MuseRoleStoryboard, "Muse không giữ literal first/last frame; đã xếp làm Storyboard tham chiếu cảnh.", true
		}
		return "", "", "", false
	}

	for i := 0; i <= len(tokens)-2; i++ {
		if t, r, h, ok := parseTypeToken(tokens[i]); ok && tokens[i] == strings.ToUpper(tokens[i]) {
			if i > 0 {
				project = normalizeProject(strings.Join(tokens[:i], "_"))
			}
			assetType, role, hint = t, r, h
			aliasStart = i + 1
			break
		}
	}
	if aliasStart < 0 || aliasStart >= len(tokens) {
		return nil
	}

	rest := tokens[aliasStart:]
	isAngle := func(tok string) bool {
		if assetType == MuseTypeLoc {
			return locAreaTokens[tok]
		}
		if assetType == MuseTypeChar || assetType == MuseTypeAnim {
			return charAngleTokens[tok]
		}
		return false
	}
	isVariant := func(tok string) bool {
		return yearTokenRe.MatchString(tok) || variantTokens[tok]
	}
	isState := func(tok string) bool {
		if assetType != MuseTypeChar && tok == "full" {
			return true
		}
		return stateTokens[tok]
	}

	splitAt := len(rest)
	for i := 1; i < len(rest); i++ {
		low := strings.ToLower(rest[i])
		if isAngle(low) || isVariant(low) || isState(low) {
			splitAt = i
			break
		}
	}
	alias := slugifyToken(strings.Join(rest[:splitAt], "_"), true)
	if alias == "" {
		return nil
	}

	var angleParts, variantParts, stateParts []string
	for _, rawTok := range rest[splitAt:] {
		low := strings.ToLower(rawTok)
		switch {
		case isAngle(low) && len(variantParts) == 0 && len(stateParts) == 0:
			angleParts = append(angleParts, low)
		case isVariant(low):
			variantParts = append(variantParts, low)
		case isState(low):
			stateParts = append(stateParts, low)
		case len(variantParts) > 0:
			variantParts = append(variantParts, slugifyToken(low, false))
		default:
			stateParts = append(stateParts, slugifyToken(low, false))
		}
	}

	angle := strings.Join(angleParts, "_")
	if role == MuseRoleCharacter && angle == "close" {
		// Still keep Character unless user narrows to Face explicitly.
	}
	if strings.EqualFold(tokens[aliasStart-1], "FF") || strings.EqualFold(tokens[aliasStart-1], "LF") {
		angle = strings.ToLower(tokens[aliasStart-1])
	}

	return &ParsedAssetName{
		Project:  project,
		Type:     assetType,
		Role:     role,
		Alias:    alias,
		Angle:    angle,
		Variant:  strings.Join(variantParts, "_"),
		State:    strings.Join(stateParts, "_"),
		Version:  version,
		Rejected: rejected,
		Hint:     hint,
	}
}

// vaultDir returns the active Asset Vault directory: user setting if set,
// otherwise the default AppData location.
func (p *Provider) vaultDir() string {
	if p != nil && p.cfg != nil {
		if dir := strings.TrimSpace(p.cfg.Get().Muse.AssetVaultDir); dir != "" {
			return dir
		}
	}
	if p != nil && p.defaultVaultDir != "" {
		return p.defaultVaultDir
	}
	if p != nil && p.root != "" {
		return filepath.Join(p.root, "assets-vault")
	}
	return ""
}

func (p *Provider) vaultThumbDir() string {
	if p != nil && p.root != "" {
		return filepath.Join(p.root, "vault-thumbs")
	}
	return ""
}

func (p *Provider) vaultFileAbs(relPath string) (string, error) {
	url := media.VaultURL(relPath)
	return p.media.VaultPath(url)
}

// uniqueVaultRelPath reserves a non-colliding relative path under vaultDir,
// bumping version (_v2, _v3...) if an unrelated file already occupies the name.
func (p *Provider) uniqueVaultRelPath(project string, assetType MuseAssetType, alias, angle, variant, state string, version int, rejected bool, ext string, ignoreAbs string) (relPath, fileName string, usedVersion int, err error) {
	v := version
	if v < 1 {
		v = 1
	}
	for attempt := 0; attempt < 500; attempt++ {
		candVer := v
		if attempt > 0 {
			candVer = max(v, 1) + attempt
		}
		name := BuildAssetFileName(project, assetType, alias, angle, variant, state, candVer, rejected, ext)
		rel := VaultRelPath(project, name, rejected)
		abs, pathErr := p.vaultFileAbs(rel)
		if pathErr != nil {
			return "", "", 0, pathErr
		}
		if ignoreAbs != "" && strings.EqualFold(filepath.Clean(abs), filepath.Clean(ignoreAbs)) {
			return rel, name, candVer, nil
		}
		if _, statErr := os.Stat(abs); errors.Is(statErr, os.ErrNotExist) {
			return rel, name, candVer, nil
		}
	}
	return "", "", 0, errors.New("Quá nhiều phiên bản trùng tên trong kho tư liệu.")
}

func copyBytesToVault(dstAbs string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return err
	}
	return utils.WriteFileAtomic(dstAbs, data, 0o644)
}

func copyFileToFile(srcAbs, dstAbs string) error {
	return copyVaultFile(srcAbs, dstAbs, os.Rename)
}

func copyVaultFile(srcAbs, dstAbs string, install func(string, string) error) error {
	in, err := os.Open(srcAbs)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dstAbs), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return install(tmpName, dstAbs)
}

func moveFileSafe(srcAbs, dstAbs string) error {
	if strings.EqualFold(filepath.Clean(srcAbs), filepath.Clean(dstAbs)) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return err
	}
	if err := os.Rename(srcAbs, dstAbs); err == nil {
		return nil
	}
	if err := copyFileToFile(srcAbs, dstAbs); err != nil {
		return err
	}
	_ = os.Remove(srcAbs)
	return nil
}

// snapshotToMedia copies a vault or media image into MediaDir under muse-ref-<sha256_16>.<ext>
// deduplicated by content hash, so queued jobs remain immutable even if the user
// edits, renames, or deletes files in the Asset Vault folder.
func (p *Provider) snapshotToMedia(localURL string) (string, error) {
	if strings.HasPrefix(localURL, media.Route) {
		return localURL, nil
	}
	srcAbs, err := p.media.OpenPath(localURL)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(srcAbs)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:8])
	ext := strings.ToLower(filepath.Ext(srcAbs))
	if ext == "" {
		ext = ".png"
	}
	id := referencePrefix + hash
	existingURL := media.Route + id + ext
	if existingPath, err := p.media.Path(existingURL); err == nil {
		if info, statErr := os.Stat(existingPath); statErr == nil && info.Mode().IsRegular() && info.Size() == int64(len(data)) {
			return existingURL, nil
		}
	}
	return p.media.Put(id, "", ext, data)
}
