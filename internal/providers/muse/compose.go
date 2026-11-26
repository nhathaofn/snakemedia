package muse

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MuseRefMode says how a batch's own reference images go to its prompts.
type MuseRefMode string

const (
	// MuseRefShared attaches every image to every prompt (the default).
	MuseRefShared MuseRefMode = "shared"
	// MuseRefPair gives prompt i image i; prompts past the last image get none.
	MuseRefPair MuseRefMode = "pair"
	// MuseRefRotate gives prompt i image i modulo the images: one each, in turn.
	MuseRefRotate MuseRefMode = "rotate"
)

var AllMuseRefModes = []struct {
	Value  MuseRefMode
	TSName string
}{{MuseRefShared, "Shared"}, {MuseRefPair, "Pair"}, {MuseRefRotate, "Rotate"}}

func (m MuseRefMode) valid() bool {
	return m == "" || m == MuseRefShared || m == MuseRefPair || m == MuseRefRotate
}

// mentionedAssets returns the assets a prompt names, in the order they first
// appear: @tag in any case, or the asset's name written exactly as saved as a
// whole word (so "Mai" does not match "ngày mai"). Every position is a byte
// offset in the prompt itself.
func mentionedAssets(prompt string, assets []MuseAsset) []MuseAsset {
	hits, _, _ := matchPromptAssets(prompt, "", assets)
	out := make([]MuseAsset, len(hits))
	for i, h := range hits {
		out[i] = h.asset
	}
	return out
}

type assetMentionHit struct {
	at           int
	asset        MuseAsset
	suffixTokens []string
	rawSuffix    string
}

func scopedAssets(assets []MuseAsset, project string) []MuseAsset {
	normProj := normalizeProject(project)
	byTag := make(map[string]int, len(assets))
	var out []MuseAsset
	// Prefer exact project match over global (_chung); when project is empty,
	// prefer global (_chung) over project-specific entries on tag collision.
	preferAsset := func(cand, prev MuseAsset) bool {
		cp := normalizeProject(cand.Project)
		pp := normalizeProject(prev.Project)
		if normProj != "" {
			return cp == normProj && pp != normProj
		}
		return cp == "" && pp != ""
	}
	for _, a := range assets {
		ap := normalizeProject(a.Project)
		if normProj != "" && ap != normProj && ap != "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(a.Tag))
		if key != "" {
			if idx, exists := byTag[key]; exists {
				if preferAsset(a, out[idx]) {
					out[idx] = a
				}
				continue
			}
			byTag[key] = len(out)
		}
		out = append(out, a)
	}
	return out
}

func splitNormalizedTokens(raw string) []string {
	s := slugifyToken(raw, false)
	if s == "" {
		return nil
	}
	var out []string
	for _, tok := range strings.Split(s, "_") {
		if tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

func matchTagMention(mention string, assets []MuseAsset) (idx int, asset *MuseAsset, suffixTokens []string, rawSuffix string) {
	// Exact tag match wins first.
	for i := range assets {
		if assets[i].Tag != "" && strings.EqualFold(mention, assets[i].Tag) {
			return i, &assets[i], nil, ""
		}
	}
	// Match @alias_angle_variant_state using the longest registered tag prefix.
	bestLen := -1
	bestIdx := -1
	for i := range assets {
		tag := assets[i].Tag
		if tag == "" || len(mention) <= len(tag)+1 {
			continue
		}
		if strings.EqualFold(mention[:len(tag)], tag) && mention[len(tag)] == '_' {
			if len(tag) > bestLen {
				bestLen = len(tag)
				bestIdx = i
				asset = &assets[i]
				rawSuffix = mention[len(tag)+1:]
				suffixTokens = splitNormalizedTokens(rawSuffix)
			}
		}
	}
	return bestIdx, asset, suffixTokens, rawSuffix
}

func matchPromptAssets(prompt, project string, assets []MuseAsset) (hits []assetMentionHit, missingTags []string, assetIdx []int) {
	pool := scopedAssets(assets, project)
	tags := tagMentions(prompt)
	matchedPoolIdx := make(map[int]bool)

	for _, m := range tags {
		idx, a, suffixTokens, rawSuffix := matchTagMention(m.tag, pool)
		if a == nil {
			if !slices.Contains(missingTags, m.tag) {
				missingTags = append(missingTags, m.tag)
			}
			continue
		}
		matchedPoolIdx[idx] = true
		hits = append(hits, assetMentionHit{
			at:           m.at,
			asset:        *a,
			suffixTokens: suffixTokens,
			rawSuffix:    rawSuffix,
		})
	}
	for i, a := range pool {
		if matchedPoolIdx[i] {
			continue
		}
		if name := strings.TrimSpace(a.Name); name != "" {
			if pos := wordIndex(prompt, name); pos >= 0 {
				hits = append(hits, assetMentionHit{at: pos, asset: a})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].at < hits[j].at })
	return hits, missingTags, nil
}

// tagMention is one @tag in a prompt and the byte offset of its @.
type tagMention struct {
	at  int
	tag string
}

// tagMentions lists the @tags of a prompt in order. An @ inside a word (an
// e-mail address) starts no tag.
func tagMentions(prompt string) []tagMention {
	var out []tagMention
	for i := 0; i < len(prompt); i++ {
		if prompt[i] != '@' {
			continue
		}
		before, _ := utf8.DecodeLastRuneInString(prompt[:i])
		if wordRune(before) {
			continue
		}
		j := i + 1
		for j < len(prompt) {
			r, size := utf8.DecodeRuneInString(prompt[j:])
			if !wordRune(r) {
				break
			}
			j += size
		}
		if j > i+1 {
			out = append(out, tagMention{at: i, tag: prompt[i+1 : j]})
		}
		i = j - 1
	}
	return out
}

// wordIndex finds word in s where it is not part of a longer word.
func wordIndex(s, word string) int {
	for from := 0; from < len(s); {
		i := strings.Index(s[from:], word)
		if i < 0 {
			return -1
		}
		i += from
		before, _ := utf8.DecodeLastRuneInString(s[:i])
		after, _ := utf8.DecodeRuneInString(s[i+len(word):])
		if !wordRune(before) && !wordRune(after) && (word[0] == '@' || before != '@') {
			return i
		}
		from = i + len(word)
	}
	return -1
}

func wordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '+')
}

func resolveAssetItems(a MuseAsset, suffixTokens []string) (selected []MuseAssetItem, extraWarnings []string) {
	if len(a.Items) == 0 && len(a.Images) > 0 {
		out := make([]MuseAssetItem, 0, len(a.Images))
		for i, u := range a.Images {
			if u != "" {
				out = append(out, MuseAssetItem{URL: u, Primary: i == 0})
			}
		}
		return out, nil
	}
	var active []MuseAssetItem
	for _, it := range a.Items {
		if !it.Rejected && it.URL != "" {
			active = append(active, it)
		}
	}
	if len(active) == 0 {
		if len(a.Items) > 0 && a.Tag != "" {
			return nil, []string{fmt.Sprintf("@%s hiện chỉ có ảnh loại (rejected), không có ảnh hợp lệ để gắn.", a.Tag)}
		}
		return nil, nil
	}

	if len(suffixTokens) > 0 {
		type scored struct {
			item  MuseAssetItem
			extra int
		}
		var matches []scored
		for _, it := range active {
			toks := assetItemTokens(it)
			if it.Version >= 1 {
				toks = append(toks, fmt.Sprintf("v%d", it.Version))
			}
			matchedAll := true
			for _, reqTok := range suffixTokens {
				if !slices.Contains(toks, reqTok) {
					matchedAll = false
					break
				}
			}
			if matchedAll {
				extra := len(toks) - len(suffixTokens)
				if extra < 0 {
					extra = 0
				}
				matches = append(matches, scored{item: it, extra: extra})
			}
		}
		if len(matches) > 0 {
			sort.SliceStable(matches, func(i, j int) bool {
				if matches[i].extra != matches[j].extra {
					return matches[i].extra < matches[j].extra
				}
				return matches[i].item.Primary && !matches[j].item.Primary
			})
			return []MuseAssetItem{matches[0].item}, nil
		}
		extraWarnings = append(extraWarnings, fmt.Sprintf("@%s chưa có biến thể %q (đã dùng ảnh chính thay thế).", a.Tag, strings.Join(suffixTokens, "_")))
	}

	var primary []MuseAssetItem
	for _, it := range active {
		if it.Primary {
			primary = append(primary, it)
		}
	}
	if len(primary) == 0 {
		primary = active[:1]
	}
	return primary, extraWarnings
}

func effectiveAssetType(a MuseAsset) MuseAssetType {
	if a.Type.valid() {
		return a.Type
	}
	if a.Role == MuseRoleNone {
		return ""
	}
	return typeForRole(a.Role)
}

func assetPriorityTier(a MuseAsset, isPrimary bool) int {
	t := effectiveAssetType(a)
	switch t {
	case MuseTypeChar:
		if isPrimary {
			return 0
		}
		return 1
	case MuseTypeAnim:
		return 2
	case MuseTypeLoc:
		return 3
	case MuseTypeVeh:
		return 4
	case MuseTypeProp, MuseTypeDoc:
		return 5
	case MuseTypeStyle:
		return 6
	default:
		return 7
	}
}

func combinedLock(assetLock, itemLock string) string {
	al := strings.TrimSpace(assetLock)
	il := strings.TrimSpace(itemLock)
	switch {
	case al != "" && il != "" && !strings.EqualFold(al, il):
		return al + "; " + il
	case al != "":
		return al
	default:
		return il
	}
}

// resolvePromptRefs computes every attached URL, structured MuseRefInfo, matched
// @tags, and advisory warnings for a prompt, ordering asset references per Section 13:
// CHAR primary -> CHAR secondary -> ANIM -> LOC -> VEH -> PROP/DOC -> STYLE -> own images.
func resolvePromptRefs(prompt, project string, index int, fixed, all []MuseAsset, own []string, mode MuseRefMode) (refs []string, info []MuseRefInfo, tags, warnings []string) {
	type plannedRef struct {
		url  string
		info MuseRefInfo
		tier int
	}
	var assetRefs []plannedRef
	seenURL := map[string]bool{}
	tags = []string{}

	addAsset := func(a MuseAsset, it MuseAssetItem) {
		if it.URL == "" || seenURL[it.URL] {
			return
		}
		seenURL[it.URL] = true
		effType := effectiveAssetType(a)
		assetRefs = append(assetRefs, plannedRef{
			url: it.URL,
			info: MuseRefInfo{
				Tag:  a.Tag,
				Role: a.Role,
				Lock: combinedLock(a.Lock, it.Lock),
				Type: string(effType),
			},
			tier: assetPriorityTier(a, it.Primary),
		})
	}

	var extraWarnings []string
	for _, a := range fixed {
		if a.Tag != "" {
			tags = appendOnce(tags, a.Tag)
		}
		items, w := resolveAssetItems(a, nil)
		extraWarnings = append(extraWarnings, w...)
		for _, it := range items {
			addAsset(a, it)
		}
	}

	hits, missingTags, _ := matchPromptAssets(prompt, project, all)
	for _, h := range hits {
		if h.asset.Tag != "" {
			tags = appendOnce(tags, h.asset.Tag)
		}
		items, w := resolveAssetItems(h.asset, h.suffixTokens)
		extraWarnings = append(extraWarnings, w...)
		for _, it := range items {
			addAsset(h.asset, it)
		}
	}

	sort.SliceStable(assetRefs, func(i, j int) bool {
		return assetRefs[i].tier < assetRefs[j].tier
	})

	for _, pr := range assetRefs {
		refs = append(refs, pr.url)
		info = append(info, pr.info)
	}

	appendOwn := func(url string) {
		if url == "" || seenURL[url] {
			return
		}
		seenURL[url] = true
		refs = append(refs, url)
		info = append(info, MuseRefInfo{})
	}
	switch {
	case len(own) == 0:
	case mode == MuseRefPair:
		if index < len(own) {
			appendOwn(own[index])
		}
	case mode == MuseRefRotate:
		appendOwn(own[index%len(own)])
	default:
		for _, url := range own {
			appendOwn(url)
		}
	}

	if !slices.ContainsFunc(info, func(ri MuseRefInfo) bool {
		return ri.Role != MuseRoleNone || ri.Lock != "" || ri.Tag != "" || ri.Type != ""
	}) {
		info = nil
	}

	warnings = refWarnings(prompt, info, missingTags)
	for _, w := range extraWarnings {
		warnings = appendOnce(warnings, w)
	}
	if warnings == nil {
		warnings = []string{}
	}
	return refs, info, tags, warnings
}

// promptRefs is what one prompt attaches, in order: the asset items ordered by
// Section 13 priority, then the batch's own images by mode.
func promptRefs(prompt string, index int, fixed, all []MuseAsset, own []string, mode MuseRefMode) (refs []string, info []MuseRefInfo) {
	refs, info, _, _ = resolvePromptRefs(prompt, "", index, fixed, all, own, mode)
	return refs, info
}

// roleGuides describe what each asset role governs concisely, leaving scene
// direction, motion, audio, and creative choices to the prompt.
var roleGuides = map[MuseRefRole]string{
	MuseRoleFace:       "character face: keep face and hair only; outfit and pose follow prompt",
	MuseRoleCharacter:  "character, full look: keep exact face, body, hair, and outfit",
	MuseRoleAnimal:     "animal/pet: keep exact species, coat pattern, and markings",
	MuseRoleProduct:    "product: keep exact shape, colors, and label text",
	MuseRoleProp:       "prop: keep exact shape, material, and colors",
	MuseRoleVehicle:    "vehicle: keep exact model, paint color, livery, and silhouette",
	MuseRoleDocument:   "document/chart: keep exact layout, labels, and readable text",
	MuseRoleStoryboard: "storyboard: follow framing, setting, and shot composition",
	MuseRoleSetting:    "setting: keep location layout and lighting only; no people from it",
	MuseRoleStyle:      "style: match color grade and rendering style only",
}

// roleLines maps numbered attached images to their @tag, concise role scope,
// and lock notes without adding directorial or priority rule blocks. first is
// the 1-based index of the first image in info (images before it are storyboard
// frames).
func roleLines(kind MuseJobKind, info []MuseRefInfo, first int) []string {
	if len(info) == 0 {
		return nil
	}
	hasTag := slices.ContainsFunc(info, func(ri MuseRefInfo) bool { return ri.Tag != "" })
	var notes []string
	if hasTag {
		notes = append(notes, "same @tag = views of one subject")
	}
	if kind == MuseJobVideo {
		notes = append(notes, "do not show reference sheets or plain backdrops as video frames")
	}
	header := "References:"
	if len(notes) > 0 {
		header = fmt.Sprintf("References (%s):", strings.Join(notes, "; "))
	}
	lines := []string{header}
	for k, ri := range info {
		n := first + k
		label := fmt.Sprintf("image %d", n)
		if ri.Tag != "" {
			label += " = @" + ri.Tag
		}
		guide := roleGuides[ri.Role]
		if guide == "" {
			guide = "reference: keep exact look; follow prompt for scene"
		}
		meta := []string{guide}
		if lock := strings.TrimSpace(ri.Lock); lock != "" {
			meta = append(meta, "lock: "+lock)
		}
		lines = append(lines, fmt.Sprintf("- %s (%s)", label, strings.Join(meta, "; ")))
	}
	return lines
}

func appendOnce(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// refWarnings are hints for one prompt's images, shown before it is queued;
// they never block it.
func refWarnings(prompt string, info []MuseRefInfo, missingTags []string) []string {
	var out []string
	roles := map[MuseRefRole]bool{}
	for _, ri := range info {
		roles[ri.Role] = true
	}
	if len(missingTags) > 0 {
		out = append(out, "Chưa có tư liệu cho: @"+strings.Join(missingTags, ", @")+" (ảnh sẽ không được gắn).")
	}
	describes := containsAny(strings.ToLower(prompt),
		"wearing", "outfit", "dress", "shirt", "jacket", "mặc", "áo", "quần", "váy", "trang phục")
	if roles[MuseRoleFace] && !roles[MuseRoleCharacter] && !roles[MuseRoleStoryboard] && !describes {
		out = append(out, "Chỉ có ảnh khuôn mặt: nên thêm ảnh toàn thân / storyboard hoặc tả trang phục, dáng người để nhân vật không bị trôi.")
	}
	if roles[MuseRoleStoryboard] && !roles[MuseRoleProduct] && !roles[MuseRoleProp] {
		out = append(out, "Có storyboard nhưng chưa có ảnh sản phẩm hoặc đạo cụ: sản phẩm / đạo cụ sẽ được vẽ theo storyboard, dễ sai màu hoặc chữ.")
	}
	return out
}

func containsAny(s string, words ...string) bool {
	for _, w := range words {
		if wordIndex(s, w) >= 0 {
			return true
		}
	}
	return false
}

// unknownTags lists the @tags in a prompt that no asset has.
func unknownTags(prompt string, assets []MuseAsset) []string {
	_, missing, _ := matchPromptAssets(prompt, "", assets)
	return missing
}
