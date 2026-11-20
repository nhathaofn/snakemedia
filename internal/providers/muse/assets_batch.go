package muse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
	"github.com/google/uuid"
)

const (
	folderPreviewMaxSide = 120
	folderPreviewQuality = 72
	folderScanWorkers    = 4
	maxTagRunes          = 40
)

// MuseAssetBatchConflict decides what ImportAssetBatch does when an @tag
// already exists in the library or repeats within the batch.
type MuseAssetBatchConflict string

const (
	MuseConflictSkip      MuseAssetBatchConflict = "skip"
	MuseConflictOverwrite MuseAssetBatchConflict = "overwrite"
	MuseConflictRename    MuseAssetBatchConflict = "rename"
)

var AllMuseAssetBatchConflicts = []struct {
	Value  MuseAssetBatchConflict
	TSName string
}{
	{MuseConflictSkip, "Skip"},
	{MuseConflictOverwrite, "Overwrite"},
	{MuseConflictRename, "Rename"},
}

func (c MuseAssetBatchConflict) valid() bool {
	return c == MuseConflictSkip || c == MuseConflictOverwrite || c == MuseConflictRename
}

// MuseFolderImage describes one image found in a picked folder before import.
type MuseFolderImage struct {
	Path       string `json:"path"`
	FileName   string `json:"fileName"`
	Size       int64  `json:"size"`
	PreviewURL string `json:"previewUrl,omitempty"`
	Error      string `json:"error,omitempty"`
}

// MuseAssetFolderScan is the naturally ordered image list of a folder.
type MuseAssetFolderScan struct {
	Canceled bool              `json:"canceled"`
	Dir      string            `json:"dir"`
	Images   []MuseFolderImage `json:"images"`
}

// MuseAssetBatchItem is one asset row sent to ImportAssetBatch.
type MuseAssetBatchItem struct {
	SourcePath string      `json:"sourcePath"`
	Tag        string      `json:"tag"`
	Name       string      `json:"name"`
	Role       MuseRefRole `json:"role"`
	Lock       string      `json:"lock"`
}

// MuseAssetBatchRequest imports a list of single-image assets at once.
type MuseAssetBatchRequest struct {
	Items    []MuseAssetBatchItem   `json:"items"`
	Conflict MuseAssetBatchConflict `json:"conflict"`
}

// MuseAssetBatchResult summarizes a completed batch import.
type MuseAssetBatchResult struct {
	Added    int      `json:"added"`
	Updated  int      `json:"updated"`
	Skipped  int      `json:"skipped"`
	SavedIDs []string `json:"savedIds"`
}

// AssetImportReady reports whether Muse limits have been read so images can
// be checked and imported.
func (p *Provider) AssetImportReady() error {
	caps, _ := p.knownCapabilities()
	if caps == nil {
		return errLimitsUnknown
	}
	return nil
}

// ScanAssetFolder reads the image files directly inside dir, sorted in natural
// filename order (matching Windows Explorer), with lightweight previews.
func (p *Provider) ScanAssetFolder(dir string) models.Response[MuseAssetFolderScan] {
	caps, _ := p.knownCapabilities()
	if caps == nil {
		return fail[MuseAssetFolderScan](errLimitsUnknown, models.CodeUnavailable)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fail[MuseAssetFolderScan](fmt.Errorf("Không đọc được thư mục %s: %w", dir, err), models.CodeInvalidArgument)
	}
	images := make([]MuseFolderImage, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if _, ok := caps.imageMime(ext); !ok {
			continue
		}
		fullPath := filepath.Join(dir, name)
		info, err := os.Stat(fullPath)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		item := MuseFolderImage{
			Path:     fullPath,
			FileName: name,
			Size:     info.Size(),
		}
		if info.Size() > caps.MaxFileBytes {
			item.Error = fmt.Sprintf("Lớn hơn %d MB (giới hạn Muse)", caps.MaxFileBytes>>20)
		}
		images = append(images, item)
	}
	slices.SortFunc(images, func(a, b MuseFolderImage) int {
		return naturalCompareFileName(a.FileName, b.FileName)
	})
	populateFolderPreviews(images)
	return models.SuccessResponse(MuseAssetFolderScan{Dir: dir, Images: images})
}

func populateFolderPreviews(images []MuseFolderImage) {
	if len(images) == 0 {
		return
	}
	jobs := make(chan int, len(images))
	for i := range images {
		if images[i].Error == "" {
			jobs <- i
		}
	}
	close(jobs)
	workers := min(folderPreviewWorkers(len(images)), len(images))
	if workers < 1 {
		return
	}
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer logger.Recover("scan asset folder", nil)
			for i := range jobs {
				images[i].PreviewURL = media.PreviewDataURL(images[i].Path, folderPreviewMaxSide, folderPreviewQuality)
			}
		}()
	}
	wg.Wait()
}

func folderPreviewWorkers(n int) int {
	if n <= 1 {
		return n
	}
	return folderScanWorkers
}

type plannedBatchAsset struct {
	sourcePath string
	asset      MuseAsset
	isUpdate   bool
}

// ImportAssetBatch validates every row, copies accepted images into the media
// store, and saves all assets to the journal in one atomic write.
func (p *Provider) ImportAssetBatch(req MuseAssetBatchRequest) models.Response[MuseAssetBatchResult] {
	caps, _ := p.knownCapabilities()
	if caps == nil {
		return fail[MuseAssetBatchResult](errLimitsUnknown, models.CodeUnavailable)
	}
	if len(req.Items) == 0 {
		return fail[MuseAssetBatchResult](errors.New("Chưa có tư liệu nào để nhập."), models.CodeInvalidArgument)
	}
	conflict := req.Conflict
	if conflict == "" {
		conflict = MuseConflictSkip
	}
	if !conflict.valid() {
		return fail[MuseAssetBatchResult](errors.New("Chế độ xử lý trùng tag không hợp lệ."), models.CodeInvalidArgument)
	}

	normalized := make([]MuseAssetBatchItem, len(req.Items))
	for i, item := range req.Items {
		tag, name, lock, err := normalizeAssetFields(item.Tag, item.Name, item.Lock)
		if err != nil {
			return fail[MuseAssetBatchResult](fmt.Errorf("Dòng %d (@%s): %w", i+1, item.Tag, err), models.CodeInvalidArgument)
		}
		if !item.Role.valid() {
			return fail[MuseAssetBatchResult](fmt.Errorf("Dòng %d (@%s): vai trò ảnh không hợp lệ.", i+1, tag), models.CodeInvalidArgument)
		}
		if _, _, _, err := checkImageFile(caps, item.SourcePath); err != nil {
			return fail[MuseAssetBatchResult](fmt.Errorf("Dòng %d (@%s): %w", i+1, tag, err), models.CodeInvalidArgument)
		}
		normalized[i] = MuseAssetBatchItem{
			SourcePath: item.SourcePath,
			Tag:        tag,
			Name:       name,
			Role:       item.Role,
			Lock:       lock,
		}
	}

	p.assetMu.Lock()
	defer p.assetMu.Unlock()

	existingByTag := make(map[string]MuseAsset)
	taken := make(map[string]bool)
	for _, a := range p.assets.List() {
		key := strings.ToLower(a.Tag)
		existingByTag[key] = a
		taken[key] = true
	}

	now := p.now().UTC()
	var plan []plannedBatchAsset
	planIndexByTag := make(map[string]int)
	skipped := 0

	for idx, item := range normalized {
		key := strings.ToLower(item.Tag)
		switch conflict {
		case MuseConflictSkip:
			if taken[key] {
				skipped++
				continue
			}
			taken[key] = true
		case MuseConflictRename:
			if taken[key] {
				item.Tag = uniqueAssetTag(item.Tag, taken)
				key = strings.ToLower(item.Tag)
			}
			taken[key] = true
		case MuseConflictOverwrite:
			taken[key] = true
		}

		if existingIdx, ok := planIndexByTag[key]; ok {
			// Overwriting an item already planned earlier in the same batch replaces it in place.
			plan[existingIdx].sourcePath = item.SourcePath
			plan[existingIdx].asset.Tag = item.Tag
			plan[existingIdx].asset.Name = item.Name
			plan[existingIdx].asset.Role = item.Role
			plan[existingIdx].asset.Lock = item.Lock
			continue
		}

		if old, exists := existingByTag[key]; exists {
			planIndexByTag[key] = len(plan)
			plan = append(plan, plannedBatchAsset{
				sourcePath: item.SourcePath,
				asset: MuseAsset{
					ID:        old.ID,
					Tag:       item.Tag,
					Name:      item.Name,
					Role:      item.Role,
					Lock:      item.Lock,
					CreatedAt: old.CreatedAt,
				},
				isUpdate: true,
			})
			continue
		}

		planIndexByTag[key] = len(plan)
		plan = append(plan, plannedBatchAsset{
			sourcePath: item.SourcePath,
			asset: MuseAsset{
				ID:        uuid.NewString(),
				Tag:       item.Tag,
				Name:      item.Name,
				Role:      item.Role,
				Lock:      item.Lock,
				CreatedAt: now.Add(time.Duration(idx) * time.Millisecond).Format(time.RFC3339),
			},
		})
	}

	if len(plan) == 0 {
		return models.SuccessResponse(MuseAssetBatchResult{Skipped: skipped, SavedIDs: []string{}})
	}

	importedURLs := make([]string, 0, len(plan))
	cleanupImported := func() {
		for _, u := range importedURLs {
			_ = p.media.Remove(u)
		}
	}

	var adds, updates []MuseAsset
	var oldImages []string
	savedIDs := make([]string, 0, len(plan))
	for _, entry := range plan {
		ref, err := p.importOneImage(caps, entry.sourcePath, assetPrefix)
		if err != nil {
			cleanupImported()
			code := models.CodeInvalidArgument
			if errors.Is(err, errImageWrite) {
				code = models.CodeInternal
			}
			return fail[MuseAssetBatchResult](err, code)
		}
		importedURLs = append(importedURLs, ref.LocalURL)
		entry.asset.Images = []string{ref.LocalURL}
		savedIDs = append(savedIDs, entry.asset.ID)
		if entry.isUpdate {
			if prev, ok := p.assets.Get(entry.asset.ID); ok {
				oldImages = append(oldImages, prev.Images...)
			}
			updates = append(updates, entry.asset)
		} else {
			adds = append(adds, entry.asset)
		}
	}

	if err := p.assets.SaveBatch(adds, updates); err != nil {
		cleanupImported()
		return fail[MuseAssetBatchResult](err, models.CodeInternal)
	}
	p.removeUnusedAssetMedia(oldImages)
	p.sweepAssetImages()
	return models.SuccessResponse(MuseAssetBatchResult{
		Added:    len(adds),
		Updated:  len(updates),
		Skipped:  skipped,
		SavedIDs: savedIDs,
	})
}

func uniqueAssetTag(tag string, taken map[string]bool) string {
	runes := []rune(tag)
	for n := 2; ; n++ {
		suffix := fmt.Sprintf("_%d", n)
		suffixRunes := utf8.RuneCountInString(suffix)
		keep := min(len(runes), maxTagRunes-suffixRunes)
		candidate := string(runes[:keep]) + suffix
		if !taken[strings.ToLower(candidate)] {
			return candidate
		}
	}
}

func naturalCompareFileName(a, b string) int {
	stemA := strings.TrimSuffix(a, filepath.Ext(a))
	stemB := strings.TrimSuffix(b, filepath.Ext(b))
	if c := naturalCompare(stemA, stemB); c != 0 {
		return c
	}
	return naturalCompare(a, b)
}

// naturalCompare sorts numeric runs by value and text case-insensitively,
// matching Windows Explorer filename order.
func naturalCompare(a, b string) int {
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		da := isASCIIDigit(a[ia])
		db := isASCIIDigit(b[ib])
		if da && db {
			ea := ia
			for ea < len(a) && isASCIIDigit(a[ea]) {
				ea++
			}
			eb := ib
			for eb < len(b) && isASCIIDigit(b[eb]) {
				eb++
			}
			if c := compareNumericRuns(a[ia:ea], b[ib:eb]); c != 0 {
				return c
			}
			ia, ib = ea, eb
			continue
		}
		ra, sa := utf8.DecodeRuneInString(a[ia:])
		rb, sb := utf8.DecodeRuneInString(b[ib:])
		la, lb := unicode.ToLower(ra), unicode.ToLower(rb)
		if la != lb {
			if la < lb {
				return -1
			}
			return 1
		}
		ia += sa
		ib += sb
	}
	if ia < len(a) {
		return 1
	}
	if ib < len(b) {
		return -1
	}
	return strings.Compare(a, b)
}

func compareNumericRuns(a, b string) int {
	na := strings.TrimLeft(a, "0")
	nb := strings.TrimLeft(b, "0")
	if len(na) != len(nb) {
		if len(na) < len(nb) {
			return -1
		}
		return 1
	}
	if na != nb {
		if na < nb {
			return -1
		}
		return 1
	}
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return 0
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
