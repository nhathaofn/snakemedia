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
	type hit struct {
		at    int
		asset MuseAsset
	}
	tags := tagMentions(prompt)
	var hits []hit
	for _, a := range assets {
		at := -1
		for _, m := range tags {
			if a.Tag != "" && strings.EqualFold(m.tag, a.Tag) {
				at = m.at
				break
			}
		}
		if name := strings.TrimSpace(a.Name); name != "" {
			if i := wordIndex(prompt, name); i >= 0 && (at < 0 || i < at) {
				at = i
			}
		}
		if at >= 0 {
			hits = append(hits, hit{at, a})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].at < hits[j].at })
	out := make([]MuseAsset, len(hits))
	for i, h := range hits {
		out[i] = h.asset
	}
	return out
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
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-')
}

// promptRefs is what one prompt attaches, in order: the fixed assets, the
// assets the prompt names, then the batch's own images by mode. Duplicates
// are dropped. info is nil when no image carries a role or lock, so such a
// job keeps the plain Reference lines.
func promptRefs(prompt string, index int, fixed, all []MuseAsset, own []string, mode MuseRefMode) (refs []string, info []MuseRefInfo) {
	seen := map[string]bool{}
	add := func(url string, ri MuseRefInfo) {
		if seen[url] {
			return
		}
		seen[url] = true
		refs = append(refs, url)
		info = append(info, ri)
	}
	assets := append(append([]MuseAsset{}, fixed...), mentionedAssets(prompt, all)...)
	for _, a := range assets {
		for _, img := range a.Images {
			add(img, MuseRefInfo{Tag: a.Tag, Role: a.Role, Lock: a.Lock})
		}
	}
	switch {
	case len(own) == 0:
	case mode == MuseRefPair:
		if index < len(own) {
			add(own[index], MuseRefInfo{})
		}
	case mode == MuseRefRotate:
		add(own[index%len(own)], MuseRefInfo{})
	default:
		for _, url := range own {
			add(url, MuseRefInfo{})
		}
	}
	if !slices.ContainsFunc(info, func(ri MuseRefInfo) bool { return ri.Role != MuseRoleNone || ri.Lock != "" || ri.Tag != "" }) {
		info = nil
	}
	return refs, info
}

// roleRule is the rule one image of a role follows.
func roleRule(role MuseRefRole, roles map[MuseRefRole]bool) string {
	faces := roles[MuseRoleFace] || roles[MuseRoleCharacter]
	switch role {
	case MuseRoleFace:
		return "identity only: keep this exact face, hair, skin tone and age for this character in every shot. Take clothing, body and setting from the other images or the prompt, not from this one, unless nothing else gives them."
	case MuseRoleCharacter:
		return "character authority: keep this exact face, body proportions, hair and outfit for this character in every shot."
	case MuseRoleProduct:
		return "sole authority for the product: its shape, colors, materials, printed text and logo exactly as shown, never redrawn or simplified. It overrides any storyboard and any product wording in the prompt."
	case MuseRoleStoryboard:
		rule := "authority for composition, camera angle, shot order, setting, lighting and wardrobe. Do not reproduce the storyboard itself (its grid, frames, numbers or sketch look)."
		if faces {
			rule += " Faces in it are placeholders: identity comes from the character images."
		}
		if roles[MuseRoleProduct] {
			rule += " Ignore any product drawn in it: the product comes only from the product image."
		}
		return rule
	case MuseRoleSetting:
		return "the place: keep its layout, materials, light and colors. Do not take people or products from it."
	case MuseRoleStyle:
		return "the look only: color grade, lighting and rendering style. Take no subject, person or product from it."
	}
	return "a separate element of the result; keep its design, colors and details exactly."
}

// roleNames words a role in the message.
var roleNames = map[MuseRefRole]string{
	MuseRoleFace: "character face", MuseRoleCharacter: "character, full look", MuseRoleProduct: "product",
	MuseRoleStoryboard: "storyboard", MuseRoleSetting: "setting", MuseRoleStyle: "style",
}

// roleLines replace the Reference lines when the job's images carry roles:
// what each numbered image is for, which wins when they disagree, and what
// must match exactly. first is the number of the first image they describe
// (images before it are storyboard frames or the source video).
func roleLines(kind MuseJobKind, info []MuseRefInfo, first int) []string {
	roles := map[MuseRefRole]bool{}
	for _, ri := range info {
		roles[ri.Role] = true
	}
	lines := []string{fmt.Sprintf("Reference roles: these attached images are references for this %s, numbered in the order attached:", kindWord[kind])}
	byRole := map[MuseRefRole][]string{}
	for k, ri := range info {
		n := first + k
		name := roleNames[ri.Role]
		if name == "" {
			name = "reference"
		}
		label := fmt.Sprintf("image %d", n)
		if ri.Tag != "" {
			label += " = @" + ri.Tag
			byRole[ri.Role] = appendOnce(byRole[ri.Role], "@"+ri.Tag)
		} else {
			byRole[ri.Role] = appendOnce(byRole[ri.Role], fmt.Sprintf("image %d", n))
		}
		lines = append(lines, fmt.Sprintf("- %s (%s): %s", label, name, roleRule(ri.Role, roles)))
	}
	lines = append(lines, "Images with the same @name show one character or object from different views: they are one subject, not several.")
	var priority []string
	if names := byRole[MuseRoleProduct]; len(names) > 0 {
		priority = append(priority, fmt.Sprintf("- Product: %s > storyboard > prompt text.", strings.Join(names, ", ")))
	}
	if names := append(append([]string{}, byRole[MuseRoleFace]...), byRole[MuseRoleCharacter]...); len(names) > 0 {
		priority = append(priority, fmt.Sprintf("- Identity: %s > faces in a storyboard > prompt text. The same person looks the same in every shot.", strings.Join(names, ", ")))
	}
	if names := byRole[MuseRoleStoryboard]; len(names) > 0 {
		priority = append(priority, fmt.Sprintf("- Scene: %s set framing, setting and wardrobe; the prompt decides actions and lines.", strings.Join(names, ", ")))
	}
	if len(priority) > 0 {
		lines = append(lines, "Priority when they disagree:")
		lines = append(lines, priority...)
	}
	lines = append(lines, "Do not copy an image's background, framing or pose unless its role says so; take those from the prompt. If the prompt itself says what an image is for, follow the prompt.")
	var locks []string
	seen := map[string]bool{}
	for _, ri := range info {
		key := ri.Tag + "\x00" + ri.Lock
		if ri.Lock == "" || seen[key] {
			continue
		}
		seen[key] = true
		subject := "@" + ri.Tag
		if ri.Tag == "" {
			subject = roleNames[ri.Role]
		}
		locks = append(locks, fmt.Sprintf("- %s: %s", subject, ri.Lock))
	}
	if len(locks) > 0 {
		lines = append(lines, "Locks (must match exactly in every frame):")
		lines = append(lines, locks...)
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
	if roles[MuseRoleStoryboard] && !roles[MuseRoleProduct] {
		out = append(out, "Có storyboard nhưng chưa có ảnh sản phẩm: sản phẩm sẽ được vẽ theo storyboard, dễ sai màu hoặc chữ.")
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
	var out []string
	for _, m := range tagMentions(prompt) {
		known := slices.ContainsFunc(assets, func(a MuseAsset) bool { return strings.EqualFold(a.Tag, m.tag) })
		if !known && !slices.Contains(out, m.tag) {
			out = append(out, m.tag)
		}
	}
	return out
}
