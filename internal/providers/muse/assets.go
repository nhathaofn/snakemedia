package muse

import (
	"errors"
	"fmt"
	"os"
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
	MuseRoleProduct    MuseRefRole = "product"
	MuseRoleStoryboard MuseRefRole = "storyboard"
	MuseRoleSetting    MuseRefRole = "setting"
	MuseRoleStyle      MuseRefRole = "style"
)

var AllMuseRefRoles = []struct {
	Value  MuseRefRole
	TSName string
}{
	{MuseRoleNone, "None"}, {MuseRoleFace, "Face"}, {MuseRoleCharacter, "Character"}, {MuseRoleProduct, "Product"},
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

// assetPrefix names the media files of the asset library. They are never
// removed by the reference clean-up, only when no asset or job uses them.
const assetPrefix = "muse-asset-"

// maxAssetImages caps the images of one asset: a few views of one face or
// product hold it better than one, and every image costs a file of the message.
const maxAssetImages = 3

// MuseAsset is a reusable reference kept by the app: a character, a product,
// a setting. A prompt names it by @Tag (or its Name, word for word) and its
// images are attached with its role and lock.
type MuseAsset struct {
	ID string `json:"id"`
	// Tag is what a prompt writes after @: letters, digits, _ and -.
	Tag  string      `json:"tag"`
	Name string      `json:"name"`
	Role MuseRefRole `json:"role"`
	// Lock lists what must match exactly (face, outfit, logo text...).
	Lock      string   `json:"lock,omitempty"`
	Images    []string `json:"images"`
	CreatedAt string   `json:"createdAt"`
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
}

var tagPattern = regexp.MustCompile(`^[\p{L}\p{N}_-]{1,40}$`)

func normalizeAsset(a *MuseAsset) {
	if a.Images == nil {
		a.Images = []string{}
	}
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
}

// ListAssets returns the asset library, oldest first.
func (p *Provider) ListAssets() models.Response[[]MuseAsset] {
	return models.SuccessResponse(p.assets.List())
}

// SaveAsset adds an asset (empty ID) or replaces one. Images must be files of
// the asset library (ImportAssetImages); images an asset no longer uses are
// deleted unless a job still attaches them.
func (p *Provider) SaveAsset(a MuseAsset) models.Response[MuseAsset] {
	a.Tag = strings.TrimPrefix(strings.TrimSpace(a.Tag), "@")
	a.Name = strings.TrimSpace(a.Name)
	a.Lock = strings.TrimSpace(a.Lock)
	if !tagPattern.MatchString(a.Tag) {
		return fail[MuseAsset](errors.New("Tag chỉ gồm chữ, số, - và _ (tối đa 40 ký tự)."), models.CodeInvalidArgument)
	}
	if !a.Role.valid() {
		return fail[MuseAsset](errors.New("Vai trò ảnh không hợp lệ."), models.CodeInvalidArgument)
	}
	if len(a.Images) == 0 || len(a.Images) > maxAssetImages {
		return fail[MuseAsset](fmt.Errorf("Mỗi tư liệu có 1 đến %d ảnh.", maxAssetImages), models.CodeInvalidArgument)
	}
	for _, img := range a.Images {
		if !strings.HasPrefix(img, media.Route+assetPrefix) {
			return fail[MuseAsset](errors.New("Ảnh tư liệu phải được thêm từ màn Tư liệu."), models.CodeInvalidArgument)
		}
	}
	for _, other := range p.assets.List() {
		if other.ID != a.ID && strings.EqualFold(other.Tag, a.Tag) {
			return fail[MuseAsset](fmt.Errorf("Tag @%s đã có.", a.Tag), models.CodeInvalidArgument)
		}
	}
	if a.ID == "" {
		a.ID = uuid.NewString()
		a.CreatedAt = p.now().UTC().Format(time.RFC3339)
		if err := p.assets.Add(a); err != nil {
			return fail[MuseAsset](err, models.CodeInternal)
		}
	} else {
		saved, err := p.assets.Update(a.ID, func(old *MuseAsset) {
			a.CreatedAt = old.CreatedAt
			*old = a
		})
		if err != nil {
			return fail[MuseAsset](err, models.CodeNotFound)
		}
		a = saved
	}
	p.sweepAssetImages()
	return models.SuccessResponse(a)
}

// RemoveAssets deletes assets; jobs already queued keep their images.
func (p *Provider) RemoveAssets(ids []string) models.Response[int] {
	n, err := p.assets.RemoveWhere(func(a MuseAsset) bool { return slices.Contains(ids, a.ID) })
	if err != nil {
		return fail[int](err, models.CodeInternal)
	}
	p.sweepAssetImages()
	return models.SuccessResponse(n)
}

// ImportAssetImages copies picked images into the media folder for an asset.
func (p *Provider) ImportAssetImages(paths []string) models.Response[[]MuseReference] {
	return p.importImages(paths, assetPrefix)
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

// assetFilesExist reports the asset images missing on disk.
func (p *Provider) missingImages(urls []string) []string {
	var missing []string
	for _, u := range urls {
		path, err := p.media.Path(u)
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
