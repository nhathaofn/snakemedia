package muse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"SnakeMedia/internal/jobs"
	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
	"github.com/google/uuid"
)

// MuseRefRole says what an attached image is for. The message gives each role
// its own rule and the roles a priority (roleLines).
type MuseRefRole string

const (
	MuseRoleNone       MuseRefRole = ""
	MuseRoleFace       MuseRefRole = "face"
	MuseRoleCharacter  MuseRefRole = "character"
	MuseRoleAnimal     MuseRefRole = "animal"
	MuseRoleProduct    MuseRefRole = "product"
	MuseRoleProp       MuseRefRole = "prop"
	MuseRoleVehicle    MuseRefRole = "vehicle"
	MuseRoleDocument   MuseRefRole = "document"
	MuseRoleStoryboard MuseRefRole = "storyboard"
	MuseRoleSetting    MuseRefRole = "setting"
	MuseRoleStyle      MuseRefRole = "style"
)

var AllMuseRefRoles = []struct {
	Value  MuseRefRole
	TSName string
}{
	{MuseRoleNone, "None"}, {MuseRoleFace, "Face"}, {MuseRoleCharacter, "Character"}, {MuseRoleAnimal, "Animal"},
	{MuseRoleProduct, "Product"}, {MuseRoleProp, "Prop"}, {MuseRoleVehicle, "Vehicle"}, {MuseRoleDocument, "Document"},
	{MuseRoleStoryboard, "Storyboard"}, {MuseRoleSetting, "Setting"}, {MuseRoleStyle, "Style"},
}

func (r MuseRefRole) valid() bool {
	return slices.ContainsFunc(AllMuseRefRoles, func(x struct {
		Value  MuseRefRole
		TSName string
	}) bool {
		return x.Value == r
	})
}

// assetPrefix names the temporary media files picked in the Asset dialog before saving.
const assetPrefix = "muse-asset-"

// maxAssetImages is 0 (unlimited): a subject in the Asset Vault may keep all
// its angles, periods, outfits, and states, while prompt attachment selects only
// the primary or explicitly requested views.
const maxAssetImages = 0

// MuseAssetItem is one canonical image file inside the Asset Vault belonging to
// a MuseAsset (e.g. _front, _full_work, _close_soot).
type MuseAssetItem struct {
	ID           string `json:"id"`
	URL          string `json:"url"`
	VaultRelPath string `json:"vaultRelPath,omitempty"`
	FileName     string `json:"fileName,omitempty"`
	Angle        string `json:"angle,omitempty"`
	Variant      string `json:"variant,omitempty"`
	State        string `json:"state,omitempty"`
	Version      int    `json:"version,omitempty"`
	Primary      bool   `json:"primary,omitempty"`
	Rejected     bool   `json:"rejected,omitempty"`
	Lock         string `json:"lock,omitempty"`
}

// MuseAsset is a reusable reference kept in the Asset Vault: a character,
// animal, location, prop, vehicle, document, or style. A prompt names it by
// @Tag (optionally suffixed with _angle_variant_state) or exact Name.
type MuseAsset struct {
	ID        string          `json:"id"`
	Project   string          `json:"project,omitempty"`
	Type      MuseAssetType   `json:"type,omitempty"`
	Tag       string          `json:"tag"`
	Name      string          `json:"name"`
	Role      MuseRefRole     `json:"role"`
	Lock      string          `json:"lock,omitempty"`
	Images    []string        `json:"images"`
	Items     []MuseAssetItem `json:"items,omitempty"`
	CreatedAt string          `json:"createdAt"`
}

// JobID implements jobs.Record.
func (a MuseAsset) JobID() string { return a.ID }

// Finished implements jobs.Record; assets are never trimmed.
func (a MuseAsset) Finished() bool { return false }

// MusePromptTemplate is a saved prompt the create screens can insert.
type MusePromptTemplate struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}

// JobID implements jobs.Record.
func (t MusePromptTemplate) JobID() string { return t.ID }

// Finished implements jobs.Record; templates are never trimmed.
func (t MusePromptTemplate) Finished() bool { return false }

// MuseRefInfo describes one attached image of a job.
type MuseRefInfo struct {
	Tag  string      `json:"tag,omitempty"`
	Role MuseRefRole `json:"role,omitempty"`
	Lock string      `json:"lock,omitempty"`
	Type string      `json:"type,omitempty"`
}

// MuseMediaAssetImportRequest adds an in-app generated image (/flow-media/...)
// directly into the Muse Asset Vault, either as a new asset or as a new
// angle/variant on an existing asset.
type MuseMediaAssetImportRequest struct {
	SourceURL        string        `json:"sourceUrl"`
	TargetAssetID    string        `json:"targetAssetId,omitempty"`
	MergeIfTagExists bool          `json:"mergeIfTagExists,omitempty"`
	Project          string        `json:"project,omitempty"`
	Type             MuseAssetType `json:"type,omitempty"`
	Tag              string        `json:"tag"`
	Name             string        `json:"name,omitempty"`
	Role             MuseRefRole   `json:"role"`
	Lock             string        `json:"lock,omitempty"`
	Angle            string        `json:"angle,omitempty"`
	Variant          string        `json:"variant,omitempty"`
	State            string        `json:"state,omitempty"`
	Primary          bool          `json:"primary,omitempty"`
	ItemLock         string        `json:"itemLock,omitempty"`
}

// MuseAssetItemPatch updates one angle/variant item inside an existing asset.
type MuseAssetItemPatch struct {
	Angle    *string `json:"angle,omitempty"`
	Variant  *string `json:"variant,omitempty"`
	State    *string `json:"state,omitempty"`
	Version  *int    `json:"version,omitempty"`
	Primary  *bool   `json:"primary,omitempty"`
	Rejected *bool   `json:"rejected,omitempty"`
	Lock     *string `json:"lock,omitempty"`
}

var tagPattern = regexp.MustCompile(`^[\p{L}\p{N}_+-]{1,40}$`)

func syncAssetImages(a *MuseAsset) {
	if a.Items == nil {
		a.Items = []MuseAssetItem{}
	}
	urls := make([]string, 0, len(a.Items))
	for _, it := range a.Items {
		if !it.Rejected && it.Primary && it.URL != "" {
			urls = append(urls, it.URL)
		}
	}
	for _, it := range a.Items {
		if !it.Rejected && !it.Primary && it.URL != "" {
			urls = append(urls, it.URL)
		}
	}
	a.Images = urls
}

func normalizeAsset(a *MuseAsset) {
	a.Project = normalizeProject(a.Project)
	if !a.Type.valid() {
		a.Type = typeForRole(a.Role)
	}
	if a.Images == nil {
		a.Images = []string{}
	}
	if len(a.Items) == 0 && len(a.Images) > 0 {
		items := make([]MuseAssetItem, 0, len(a.Images))
		for i, u := range a.Images {
			items = append(items, MuseAssetItem{
				ID:       fmt.Sprintf("legacy-%d", i+1),
				URL:      u,
				FileName: filepath.Base(u),
				Version:  1,
				Primary:  i == 0,
			})
		}
		a.Items = items
	}
	syncAssetImages(a)
}

func newAssets(path string) *jobs.Journal[MuseAsset] {
	return jobs.New(path, jobs.Options[MuseAsset]{Normalize: normalizeAsset})
}

func newTemplates(path string) *jobs.Journal[MusePromptTemplate] {
	return jobs.New(path, jobs.Options[MusePromptTemplate]{})
}

// loadStudio reads the asset library and the prompt templates; an unreadable
// file is moved aside by the journal and the library starts empty.
func (p *Provider) loadStudio() {
	if err := p.assets.Load(); err != nil {
		logger.Get().Warn("muse assets reset", "error", err)
	}
	if err := p.templates.Load(); err != nil {
		logger.Get().Warn("muse templates reset", "error", err)
	}
	p.migrateLegacyAssetsToVault()
}

// migrateLegacyAssetsToVault copies older /flow-media/muse-asset-* files into
// the managed Asset Vault on first load while keeping legacy fallback intact.
func (p *Provider) migrateLegacyAssetsToVault() {
	if p.media == nil || p.vaultDir() == "" {
		return
	}
	p.assetMu.Lock()
	defer p.assetMu.Unlock()
	var updates []MuseAsset
	var files []assetFileChanges
	for _, a := range p.assets.List() {
		needsMigrate := false
		for _, it := range a.Items {
			if strings.HasPrefix(it.URL, media.Route+assetPrefix) {
				needsMigrate = true
				break
			}
		}
		if !needsMigrate {
			continue
		}
		orig := a
		if change, err := p.materializeAssetItems(&a, &orig); err == nil {
			updates = append(updates, a)
			files = append(files, change)
		}
	}
	if len(updates) > 0 {
		if err := p.assets.SaveBatch(nil, updates); err != nil {
			for _, change := range files {
				change.rollback()
			}
			logger.Get().Warn("muse legacy asset vault migration failed", "error", err)
			return
		}
		for _, change := range files {
			p.removeUnusedVaultFiles(change.replaced)
		}
	}
}

// ListAssets returns the asset library, oldest first.
func (p *Provider) ListAssets() models.Response[[]MuseAsset] {
	return models.SuccessResponse(p.assets.List())
}

// normalizeAssetFields trims user text and strips a leading @ from the tag.
func normalizeAssetFields(tag, name, lock string) (string, string, string, error) {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "@")
	name = strings.TrimSpace(name)
	lock = strings.TrimSpace(lock)
	if !tagPattern.MatchString(tag) {
		return "", "", "", errors.New("Tag chỉ gồm chữ, số, +, - và _ (tối đa 40 ký tự).")
	}
	return tag, name, lock, nil
}

func assetScopeKey(project, tag string) string {
	tagKey := slugifyToken(tag, true)
	if tagKey == "" {
		tagKey = strings.ToLower(strings.TrimSpace(tag))
	}
	return normalizeProject(project) + "|" + tagKey
}

func assetItemTokens(it MuseAssetItem) []string {
	var parts []string
	for _, raw := range []string{it.Angle, it.Variant, it.State} {
		if s := slugifyToken(raw, false); s != "" {
			parts = append(parts, strings.Split(s, "_")...)
		}
	}
	return parts
}

func defaultPrimaryIndex(items []MuseAssetItem) int {
	firstActive := -1
	for i, it := range items {
		if it.Rejected {
			continue
		}
		if firstActive < 0 {
			firstActive = i
		}
		if strings.EqualFold(strings.TrimSpace(it.Angle), "front") {
			return i
		}
	}
	return firstActive
}

func ensurePrimaryItem(items []MuseAssetItem) {
	hasPrimary := false
	for i := range items {
		if items[i].Rejected {
			items[i].Primary = false
			continue
		}
		if items[i].Primary {
			if hasPrimary {
				items[i].Primary = false
				continue
			}
			hasPrimary = true
		}
	}
	if !hasPrimary {
		if idx := defaultPrimaryIndex(items); idx >= 0 {
			items[idx].Primary = true
		}
	}
}

// SaveAsset adds an asset (empty ID) or replaces one, keeping files inside the
// Asset Vault named cleanly according to the naming convention.
func (p *Provider) SaveAsset(a MuseAsset) models.Response[MuseAsset] {
	tag, name, lock, err := normalizeAssetFields(a.Tag, a.Name, a.Lock)
	if err != nil {
		return fail[MuseAsset](err, models.CodeInvalidArgument)
	}
	a.Project = normalizeProject(a.Project)
	a.Tag, a.Name, a.Lock = tag, name, lock
	if !a.Role.valid() {
		return fail[MuseAsset](errors.New("Vai trò ảnh không hợp lệ."), models.CodeInvalidArgument)
	}
	if !a.Type.valid() {
		a.Type = typeForRole(a.Role)
	}
	if len(a.Items) == 0 && len(a.Images) == 0 {
		return fail[MuseAsset](errors.New("Mỗi tư liệu cần ít nhất 1 ảnh."), models.CodeInvalidArgument)
	}

	p.assetMu.Lock()
	defer p.assetMu.Unlock()
	return p.saveAssetLocked(a)
}

func (p *Provider) saveAssetLocked(a MuseAsset) models.Response[MuseAsset] {
	key := assetScopeKey(a.Project, a.Tag)
	for _, other := range p.assets.List() {
		if other.ID != a.ID && assetScopeKey(other.Project, other.Tag) == key {
			if a.Project != "" {
				return fail[MuseAsset](fmt.Errorf("Tag @%s đã có trong dự án %s.", a.Tag, a.Project), models.CodeInvalidArgument)
			}
			return fail[MuseAsset](fmt.Errorf("Tag @%s đã có.", a.Tag), models.CodeInvalidArgument)
		}
	}

	var oldPtr *MuseAsset
	if a.ID != "" {
		if existing, ok := p.assets.Get(a.ID); ok {
			oldPtr = &existing
			a.CreatedAt = existing.CreatedAt
		} else {
			return fail[MuseAsset](errors.New("Tư liệu không còn tồn tại."), models.CodeNotFound)
		}
	}

	files, matErr := p.materializeAssetItems(&a, oldPtr)
	if matErr != nil {
		return fail[MuseAsset](matErr, models.CodeInvalidArgument)
	}

	if a.ID == "" {
		a.ID = uuid.NewString()
		a.CreatedAt = p.now().UTC().Format(time.RFC3339)
		if err := p.assets.Add(a); err != nil {
			files.rollback()
			return fail[MuseAsset](err, models.CodeInternal)
		}
	} else {
		saved, err := p.assets.Update(a.ID, func(old *MuseAsset) {
			a.CreatedAt = old.CreatedAt
			*old = a
		})
		if err != nil {
			files.rollback()
			return fail[MuseAsset](err, models.CodeInternal)
		}
		a = saved
	}

	p.finishAssetFiles([]assetFileChanges{files})
	return models.SuccessResponse(a)
}

// ImportAssetFromMedia saves an in-app generated image (/flow-media/...) directly
// into the Muse Asset Vault with canonical naming, preserving the original file format.
func (p *Provider) ImportAssetFromMedia(req MuseMediaAssetImportRequest) models.Response[MuseAsset] {
	srcPath, err := p.media.Path(req.SourceURL)
	if err != nil {
		return fail[MuseAsset](err, models.CodeInvalidArgument)
	}
	info, statErr := os.Stat(srcPath)
	if statErr != nil || !info.Mode().IsRegular() {
		return fail[MuseAsset](errors.New("Ảnh nguồn không còn trong thư mục media."), models.CodeNotFound)
	}
	if !req.Role.valid() {
		return fail[MuseAsset](errors.New("Vai trò ảnh không hợp lệ."), models.CodeInvalidArgument)
	}

	newItem := MuseAssetItem{
		ID:      uuid.NewString(),
		URL:     req.SourceURL,
		Angle:   req.Angle,
		Variant: req.Variant,
		State:   req.State,
		Version: 1,
		Primary: req.Primary,
		Lock:    strings.TrimSpace(req.ItemLock),
	}

	p.assetMu.Lock()
	defer p.assetMu.Unlock()
	var target MuseAsset
	foundTarget := false
	if strings.TrimSpace(req.TargetAssetID) != "" {
		if existing, ok := p.assets.Get(req.TargetAssetID); ok {
			target = existing
			foundTarget = true
		} else {
			return fail[MuseAsset](errors.New("Tư liệu đích không còn tồn tại."), models.CodeNotFound)
		}
	} else {
		proj := normalizeProject(req.Project)
		tag, _, _, tagErr := normalizeAssetFields(req.Tag, req.Name, req.Lock)
		if tagErr != nil {
			return fail[MuseAsset](tagErr, models.CodeInvalidArgument)
		}
		if req.MergeIfTagExists {
			key := assetScopeKey(proj, tag)
			for _, other := range p.assets.List() {
				if assetScopeKey(other.Project, other.Tag) == key {
					target = other
					foundTarget = true
					break
				}
			}
		}
	}

	if foundTarget {
		if req.Primary {
			for i := range target.Items {
				target.Items[i].Primary = false
			}
		}
		target.Items = append(target.Items, newItem)
		return p.saveAssetLocked(target)
	}

	tag, name, lock, tagErr := normalizeAssetFields(req.Tag, req.Name, req.Lock)
	if tagErr != nil {
		return fail[MuseAsset](tagErr, models.CodeInvalidArgument)
	}
	assetType := req.Type
	if !assetType.valid() {
		assetType = typeForRole(req.Role)
	}
	return p.saveAssetLocked(MuseAsset{
		Project: normalizeProject(req.Project),
		Type:    assetType,
		Tag:     tag,
		Name:    name,
		Role:    req.Role,
		Lock:    lock,
		Items:   []MuseAssetItem{newItem},
	})
}

// UpdateAssetItem updates one angle/variant item inside an existing asset,
// renaming or moving the file in the Asset Vault automatically.
func (p *Provider) UpdateAssetItem(assetID, itemID string, patch MuseAssetItemPatch) models.Response[MuseAsset] {
	p.assetMu.Lock()
	defer p.assetMu.Unlock()
	existing, ok := p.assets.Get(assetID)
	if !ok {
		return fail[MuseAsset](errors.New("Tư liệu không tồn tại."), models.CodeNotFound)
	}
	idx := slices.IndexFunc(existing.Items, func(it MuseAssetItem) bool { return it.ID == itemID })
	if idx < 0 {
		return fail[MuseAsset](errors.New("Ảnh thành phần không tồn tại."), models.CodeNotFound)
	}
	existing.Items = slices.Clone(existing.Items)
	if patch.Angle != nil {
		existing.Items[idx].Angle = *patch.Angle
	}
	if patch.Variant != nil {
		existing.Items[idx].Variant = *patch.Variant
	}
	if patch.State != nil {
		existing.Items[idx].State = *patch.State
	}
	if patch.Version != nil {
		existing.Items[idx].Version = max(*patch.Version, 1)
	}
	if patch.Lock != nil {
		existing.Items[idx].Lock = *patch.Lock
	}
	if patch.Rejected != nil {
		existing.Items[idx].Rejected = *patch.Rejected
		if *patch.Rejected {
			existing.Items[idx].Primary = false
		}
	}
	if patch.Primary != nil && *patch.Primary && !existing.Items[idx].Rejected {
		for i := range existing.Items {
			existing.Items[i].Primary = i == idx
		}
	}
	return p.saveAssetLocked(existing)
}

// RemoveAssets deletes assets and immediately frees their unused asset images
// from disk; images still referenced directly by jobs stay intact.
func (p *Provider) RemoveAssets(ids []string) models.Response[int] {
	p.assetMu.Lock()
	defer p.assetMu.Unlock()
	var droppedMedia []string
	var droppedVault []string
	n, err := p.assets.RemoveWhere(func(a MuseAsset) bool {
		if !slices.Contains(ids, a.ID) {
			return false
		}
		droppedMedia = append(droppedMedia, a.Images...)
		for _, it := range a.Items {
			if strings.HasPrefix(it.URL, media.VaultRoute) {
				if abs, pathErr := p.media.VaultPath(it.URL); pathErr == nil {
					droppedVault = append(droppedVault, abs)
				}
			} else if strings.HasPrefix(it.URL, media.Route+assetPrefix) {
				droppedMedia = append(droppedMedia, it.URL)
			}
		}
		return true
	})
	if err != nil {
		return fail[int](err, models.CodeInternal)
	}
	p.removeUnusedAssetMedia(droppedMedia)
	p.removeUnusedVaultFiles(droppedVault)
	p.sweepAssetImages()
	return models.SuccessResponse(n)
}

// UpdateAssetsRole assigns role (and matching canonical Type/filenames) to
// all selected assets in a single write.
func (p *Provider) UpdateAssetsRole(ids []string, role MuseRefRole) models.Response[int] {
	if !role.valid() {
		return fail[int](errors.New("Vai trò ảnh không hợp lệ."), models.CodeInvalidArgument)
	}
	if len(ids) == 0 {
		return models.SuccessResponse(0)
	}
	newType := typeForRole(role)
	p.assetMu.Lock()
	defer p.assetMu.Unlock()
	selected := p.assets.Snapshot(ids)
	var updates []MuseAsset
	var files []assetFileChanges
	committed := false
	defer func() {
		if !committed {
			for _, change := range files {
				change.rollback()
			}
		}
	}()
	for _, id := range ids {
		a, ok := selected[id]
		if !ok {
			continue
		}
		delete(selected, id)
		if a.Role == role && (!newType.valid() || a.Type == newType) {
			continue
		}
		orig := a
		a.Role = role
		if newType.valid() {
			a.Type = newType
		}
		if len(a.Items) > 0 || len(a.Images) > 0 {
			change, err := p.materializeAssetItems(&a, &orig)
			if err != nil {
				return fail[int](err, models.CodeInternal)
			}
			files = append(files, change)
		}
		updates = append(updates, a)
	}
	if len(updates) == 0 {
		return models.SuccessResponse(0)
	}
	if err := p.assets.SaveBatch(nil, updates); err != nil {
		return fail[int](err, models.CodeInternal)
	}
	committed = true
	p.finishAssetFiles(files)
	return models.SuccessResponse(len(updates))
}

// ImportAssetImages copies picked images into the temporary media folder before SaveAsset.
func (p *Provider) ImportAssetImages(paths []string) models.Response[[]MuseReference] {
	return p.importImages(paths, assetPrefix)
}

func (p *Provider) removeUnusedVaultFiles(absCandidates []string) {
	if len(absCandidates) == 0 {
		return
	}
	usedURLs := p.usedAssetImages(p.jobs.list())
	usedAbs := make(map[string]bool, len(usedURLs))
	for u := range usedURLs {
		if strings.HasPrefix(u, media.VaultRoute) {
			if abs, err := p.media.VaultPath(u); err == nil {
				usedAbs[strings.ToLower(filepath.Clean(abs))] = true
			}
		}
	}
	seen := make(map[string]bool, len(absCandidates))
	for _, abs := range absCandidates {
		key := strings.ToLower(filepath.Clean(abs))
		if seen[key] || usedAbs[key] {
			continue
		}
		seen[key] = true
		if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Get().Warn("muse vault image not removed", "path", abs, "error", err)
		}
	}
}

// removeUnusedAssetMedia immediately deletes candidate asset files (and their
// cached thumbnails) that no remaining asset and no job still references.
func (p *Provider) removeUnusedAssetMedia(candidates []string) {
	if len(candidates) == 0 {
		return
	}
	used := p.usedAssetImages(p.jobs.list())
	seen := make(map[string]bool, len(candidates))
	for _, url := range candidates {
		if seen[url] || used[url] || !strings.HasPrefix(url, media.Route+assetPrefix) {
			continue
		}
		seen[url] = true
		if err := p.media.Remove(url); err != nil {
			logger.Get().Warn("muse asset media not removed", "url", url, "error", err)
		}
	}
}

// sweepAssetImages deletes asset images no asset and no job uses. An image
// imported but not saved yet is kept for referenceRetention, like references.
func (p *Provider) sweepAssetImages() {
	p.sweepMedia(assetPrefix, p.usedAssetImages(p.jobs.list()), p.readMediaDir())
}

// usedAssetImages is the set of images assets or jobs use.
func (p *Provider) usedAssetImages(jobs []MuseJob) map[string]bool {
	used := map[string]bool{}
	for _, a := range p.assets.List() {
		for _, img := range a.Images {
			used[img] = true
		}
		for _, it := range a.Items {
			if it.URL != "" {
				used[it.URL] = true
			}
		}
	}
	for _, j := range jobs {
		for _, ref := range j.References {
			used[ref] = true
		}
	}
	return used
}

// ListTemplates returns the saved prompts, oldest first.
func (p *Provider) ListTemplates() models.Response[[]MusePromptTemplate] {
	return models.SuccessResponse(p.templates.List())
}

// SaveTemplate adds (empty ID) or replaces a saved prompt.
func (p *Provider) SaveTemplate(t MusePromptTemplate) models.Response[MusePromptTemplate] {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || strings.TrimSpace(t.Text) == "" {
		return fail[MusePromptTemplate](errors.New("Mẫu cần tên và nội dung."), models.CodeInvalidArgument)
	}
	if t.ID == "" {
		t.ID = uuid.NewString()
		t.CreatedAt = p.now().UTC().Format(time.RFC3339)
		if err := p.templates.Add(t); err != nil {
			return fail[MusePromptTemplate](err, models.CodeInternal)
		}
		return models.SuccessResponse(t)
	}
	saved, err := p.templates.Update(t.ID, func(old *MusePromptTemplate) { old.Name, old.Text = t.Name, t.Text })
	if err != nil {
		return fail[MusePromptTemplate](err, models.CodeNotFound)
	}
	return models.SuccessResponse(saved)
}

// RemoveTemplate deletes a saved prompt.
func (p *Provider) RemoveTemplate(id string) models.Response[bool] {
	if err := p.templates.Remove(id); err != nil {
		return fail[bool](err, models.CodeNotFound)
	}
	return models.SuccessResponse(true)
}

// missingImages reports the asset or media images missing on disk.
func (p *Provider) missingImages(urls []string) []string {
	var missing []string
	for _, u := range urls {
		path, err := p.media.OpenPath(u)
		if err != nil {
			missing = append(missing, u)
			continue
		}
		if _, err := os.Stat(path); err != nil {
			missing = append(missing, u)
		}
	}
	return missing
}
