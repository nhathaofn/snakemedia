package muse

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"SnakeMedia/pkg/utils"
	"github.com/google/uuid"
)

type StudioProject struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Paused    bool   `json:"paused"`
	CreatedAt string `json:"createdAt"`
}

type AssetRole string

const (
	RoleCharacterFace AssetRole = "character_face"
	RoleCharacterFull AssetRole = "character_full"
	RoleStoryboard    AssetRole = "storyboard"
	RoleProduct       AssetRole = "product"
	RoleOther         AssetRole = "other"
)

type StudioAsset struct {
	ID        string    `json:"id"`
	Tag       string    `json:"tag"`
	Name      string    `json:"name"`
	Role      AssetRole `json:"role"`
	LockNotes string    `json:"lockNotes,omitempty"`
	Images    []string  `json:"images"` // ≤3 media URLs/local paths
	ProjectID string    `json:"projectId,omitempty"`
	CreatedAt string    `json:"createdAt"`
}

type PromptTemplate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Text string `json:"text"`
}

type StudioModelConfig struct {
	On     bool     `json:"on"`
	Images []string `json:"images"` // ≤3 images
}

type StudioRotateConfig struct {
	On     bool     `json:"on"`
	Images []string `json:"images"` // ≤20 images
	Index  int      `json:"index"`
}

type StudioData struct {
	ActiveProject   string             `json:"activeProject"`
	Projects        []StudioProject    `json:"projects"`
	Assets          []StudioAsset      `json:"assets"`
	PromptTemplates []PromptTemplate   `json:"promptTemplates"`
	ModelConfig     StudioModelConfig  `json:"modelConfig"`
	RotateConfig    StudioRotateConfig `json:"rotateConfig"`
}

type studioStore struct {
	mu     sync.Mutex
	path   string
	data   StudioData
	loaded bool
	err    error
}

func newStudioStore(path string) *studioStore {
	return &studioStore{path: path}
}

func (s *studioStore) loadLocked() error {
	if s.err != nil || s.loaded {
		return s.err
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.data = StudioData{
			ActiveProject: "default",
			Projects: []StudioProject{
				{ID: "default", Name: "Mặc định", Paused: false, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
			},
			Assets:          []StudioAsset{},
			PromptTemplates: defaultPromptTemplates(),
		}
		s.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	var res StudioData
	if err := json.Unmarshal(data, &res); err != nil {
		return err
	}
	if res.ActiveProject == "" {
		res.ActiveProject = "default"
	}
	if len(res.Projects) == 0 {
		res.Projects = []StudioProject{
			{ID: "default", Name: "Mặc định", Paused: false, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
		}
	}
	if res.Assets == nil {
		res.Assets = []StudioAsset{}
	}
	if res.ModelConfig.Images == nil {
		res.ModelConfig.Images = []string{}
	}
	if res.RotateConfig.Images == nil {
		res.RotateConfig.Images = []string{}
	}
	if len(res.PromptTemplates) == 0 {
		res.PromptTemplates = defaultPromptTemplates()
	}
	s.data = res
	s.loaded = true
	return nil
}

func defaultPromptTemplates() []PromptTemplate {
	return []PromptTemplate{
		{
			ID:   "zoom-shirt",
			Name: "Zoom áo (Push-in / Pull-out)",
			Text: `Replace the character's t-shirt with the design in the attached reference image, keeping 100% accurate graphics, readable typography, and colors without distortion.
Camera movement & timeline:
- Camera fixed on a steady tripod, character standing naturally with friendly and cheerful demeanor.
- 0s to 2s: Medium portrait shot showing the character and casual t-shirt.
- 2s to 5s: Smooth, slow optical push-in zoom into the chest area, macro close-up showcasing the graphic t-shirt artwork in crisp 4K detail, realistic woven cotton fabric texture.
- 5s to 8s: Smooth, slow optical pull-out zoom returning back to the original medium portrait shot.
Audio: No spoken dialogue, only authentic natural soft laughter and pleasant room ambience.
Visual quality: Cinematic commercial lighting, ultra-sharp focus, natural cloth folds, highly detailed rendering.`,
		},
		{
			ID:   "restyle-shirt",
			Name: "Thay áo (Restyle)",
			Text: `Recreate the scene following the camera angle and motion of the reference video:
- Seamlessly replace the character's outfit with the casual t-shirt from the attached image, keeping the graphic print, typography, and colors sharp and clearly visible.
- Keep fixed tripod framing, natural studio lighting, authentic character movement, and gentle cheerful laughter without spoken words.`,
		},
	}
}

func (s *studioStore) get() StudioData {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadLocked()
	res := s.data
	if res.Projects == nil {
		res.Projects = []StudioProject{
			{ID: "default", Name: "Mặc định", Paused: false, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
		}
	}
	if res.Assets == nil {
		res.Assets = []StudioAsset{}
	}
	if res.ModelConfig.Images == nil {
		res.ModelConfig.Images = []string{}
	}
	if res.RotateConfig.Images == nil {
		res.RotateConfig.Images = []string{}
	}
	if len(res.PromptTemplates) == 0 {
		res.PromptTemplates = defaultPromptTemplates()
	}
	return res
}

func (s *studioStore) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return utils.WriteFileAtomic(s.path, raw, 0o600)
}

func (s *studioStore) save(data StudioData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	s.data = data
	return s.saveLocked()
}

func (s *studioStore) addAsset(asset StudioAsset) (StudioAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return StudioAsset{}, err
	}
	if asset.ID == "" {
		asset.ID = uuid.NewString()
	}
	cleanTag := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(asset.Tag), "@"))
	if cleanTag == "" {
		return StudioAsset{}, errors.New("Tag không được để trống")
	}
	asset.Tag = cleanTag
	asset.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	if len(asset.Images) > 3 {
		asset.Images = asset.Images[:3]
	}
	s.data.Assets = append(s.data.Assets, asset)
	return asset, s.saveLocked()
}

func (s *studioStore) removeAsset(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	var updated []StudioAsset
	for _, a := range s.data.Assets {
		if a.ID != id {
			updated = append(updated, a)
		}
	}
	s.data.Assets = updated
	return s.saveLocked()
}

func (s *studioStore) updateAsset(asset StudioAsset) (StudioAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return StudioAsset{}, err
	}
	cleanTag := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(asset.Tag), "@"))
	if cleanTag == "" {
		return StudioAsset{}, errors.New("Tag không được để trống")
	}
	asset.Tag = cleanTag
	if len(asset.Images) > 3 {
		asset.Images = asset.Images[:3]
	}
	found := false
	for i, a := range s.data.Assets {
		if a.ID == asset.ID {
			s.data.Assets[i] = asset
			found = true
			break
		}
	}
	if !found {
		return StudioAsset{}, errors.New("Không tìm thấy tư liệu")
	}
	return asset, s.saveLocked()
}

var tagMentionRe = regexp.MustCompile(`@([\p{L}\p{N}_-]+)`)

// findAssets finds matched studio assets appearing in prompt by tag or name.
func findAssets(prompt string, assets []StudioAsset) ([]StudioAsset, []string) {
	promptLower := strings.ToLower(prompt)
	matches := tagMentionRe.FindAllStringSubmatch(prompt, -1)
	var orderedTags []string
	seenTag := make(map[string]bool)
	for _, m := range matches {
		if len(m) == 2 {
			tag := strings.ToLower(m[1])
			if !seenTag[tag] {
				seenTag[tag] = true
				orderedTags = append(orderedTags, tag)
			}
		}
	}

	var matched []StudioAsset
	var missingTags []string

	for _, tag := range orderedTags {
		found := false
		for _, a := range assets {
			if strings.ToLower(a.Tag) == tag {
				matched = append(matched, a)
				found = true
				break
			}
		}
		if !found {
			missingTags = append(missingTags, tag)
		}
	}

	// Also match asset by exact name if not already matched
	for _, a := range assets {
		if a.Name == "" || seenTag[strings.ToLower(a.Tag)] {
			continue
		}
		nameLower := strings.ToLower(a.Name)
		if strings.Contains(promptLower, nameLower) {
			seenTag[strings.ToLower(a.Tag)] = true
			matched = append(matched, a)
		}
	}

	return matched, missingTags
}

// untag removes '@' prefix before known tags in the prompt before submitting to Muse.
func untag(prompt string, assets []StudioAsset) string {
	res := prompt
	for _, a := range assets {
		if a.Tag != "" {
			re := regexp.MustCompile(`(?i)@` + regexp.QuoteMeta(a.Tag))
			res = re.ReplaceAllString(res, a.Tag)
		}
	}
	return res
}

// ComposeImagesResult is the output of composeImages.
type ComposeImagesResult struct {
	Images       []string
	Assets       []StudioAsset
	Manifest     string
	UntaggedText string
}

// composeImages resolves model, @tags, rotation and batch references into final reference list and manifest.
func composeImages(prompt string, batchIdx int, studio StudioData, batchRefs []string, pairMode bool, maxImages int) (*ComposeImagesResult, error) {
	matchedAssets, missingTags := findAssets(prompt, studio.Assets)
	if len(missingTags) > 0 {
		return nil, fmt.Errorf("Tag @%s không tồn tại trong kho tư liệu", strings.Join(missingTags, ", @"))
	}

	var list []string

	// 1. Fixed model images
	if studio.ModelConfig.On {
		list = append(list, studio.ModelConfig.Images...)
	}

	// 2. Asset images in order
	for _, a := range matchedAssets {
		list = append(list, a.Images...)
	}

	// 3. Rotating image
	if studio.RotateConfig.On && len(studio.RotateConfig.Images) > 0 {
		idx := (studio.RotateConfig.Index + batchIdx) % len(studio.RotateConfig.Images)
		list = append(list, studio.RotateConfig.Images[idx])
	}

	// 4. Batch references (pair or shared)
	if pairMode {
		if batchIdx < len(batchRefs) {
			list = append(list, batchRefs[batchIdx])
		}
	} else {
		list = append(list, batchRefs...)
	}

	// Deduplicate preserving order
	var unique []string
	seen := make(map[string]bool)
	for _, img := range list {
		clean := strings.TrimSpace(img)
		if clean != "" && !seen[clean] {
			seen[clean] = true
			unique = append(unique, clean)
		}
	}

	if maxImages <= 0 {
		maxImages = MaxReferences
	}
	if len(unique) > maxImages {
		unique = unique[:maxImages]
	}

	manifest := buildRefManifest(matchedAssets)
	untagged := untag(prompt, matchedAssets)

	return &ComposeImagesResult{
		Images:       unique,
		Assets:       matchedAssets,
		Manifest:     manifest,
		UntaggedText: untagged,
	}, nil
}

// buildRefManifest constructs the system priority block for reference images.
func buildRefManifest(refs []StudioAsset) string {
	if len(refs) == 0 {
		return ""
	}

	roleLabel := map[AssetRole]string{
		RoleCharacterFace: "character face",
		RoleCharacterFull: "character full body",
		RoleStoryboard:    "storyboard",
		RoleProduct:       "product",
		RoleOther:         "general reference",
	}

	hasProduct := false
	hasFace := false
	for _, r := range refs {
		if r.Role == RoleProduct {
			hasProduct = true
		}
		if r.Role == RoleCharacterFace || r.Role == RoleCharacterFull {
			hasFace = true
		}
	}

	ruleFor := func(r StudioAsset) string {
		switch r.Role {
		case RoleCharacterFace:
			return "identity lock ONLY: keep this exact face, hair, skin tone and age for the character in every shot. Do not take clothing, body or setting from this image unless nothing else specifies them."
		case RoleCharacterFull:
			return "character authority: keep this exact face, body proportions, hair and outfit for the character in every shot."
		case RoleStoryboard:
			rule := "authority for composition, camera angles, shot order, setting, vehicle/room, lighting and wardrobe."
			if hasFace {
				rule += " Faces drawn here are only placeholders: use the character reference image(s) for identity."
			}
			if hasProduct {
				rule += " IGNORE any product drawn in this storyboard (shape and color): the product comes ONLY from the product image."
			}
			return rule
		case RoleProduct:
			return "SOLE authority for the product: shape, color, material, details. Overrides the storyboard AND any product wording in the prompt."
		default:
			return "general reference."
		}
	}

	var lines []string
	lines = append(lines, "=== REFERENCE ROLE & PRIORITY PROTOCOL (system-inserted by SnakeMedia, not part of the user prompt) ===")
	lines = append(lines, fmt.Sprintf("Attached images, in this exact order (%d):", len(refs)))
	for i, r := range refs {
		label := roleLabel[r.Role]
		if label == "" {
			label = "unspecified"
		}
		lines = append(lines, fmt.Sprintf("- Image %d = @%s (%s): %s", i+1, r.Tag, label, ruleFor(r)))
	}

	var products, storyboards, faces, locks []StudioAsset
	for _, r := range refs {
		switch r.Role {
		case RoleProduct:
			products = append(products, r)
		case RoleStoryboard:
			storyboards = append(storyboards, r)
		case RoleCharacterFace, RoleCharacterFull:
			faces = append(faces, r)
		}
		if strings.TrimSpace(r.LockNotes) != "" {
			locks = append(locks, r)
		}
	}

	lines = append(lines, "Priority rules:")
	if len(products) > 0 {
		var tags []string
		for _, p := range products {
			tags = append(tags, "@"+p.Tag)
		}
		lines = append(lines, fmt.Sprintf("- PRODUCT: %s > storyboard > prompt text. If the prompt or the storyboard describes the product differently (color, shape), follow the product image.", strings.Join(tags, ", ")))
	}
	if len(faces) > 0 {
		var tags []string
		for _, f := range faces {
			tags = append(tags, "@"+f.Tag)
		}
		lines = append(lines, fmt.Sprintf("- IDENTITY: %s > storyboard faces > prompt text.", strings.Join(tags, ", ")))
	}
	if len(storyboards) > 0 {
		var tags []string
		for _, s := range storyboards {
			tags = append(tags, "@"+s.Tag)
		}
		lines = append(lines, fmt.Sprintf("- SCENE: %s define framing, setting and wardrobe; do not render the storyboard grid itself.", strings.Join(tags, ", ")))
	}
	if len(locks) > 0 {
		lines = append(lines, "Locks (must match exactly):")
		for _, l := range locks {
			lines = append(lines, fmt.Sprintf("- @%s lock: %s", l.Tag, l.LockNotes))
		}
	}
	lines = append(lines, "=== END PROTOCOL ===")
	return strings.Join(lines, "\n")
}
