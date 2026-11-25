package muse

import (
	"errors"
	"fmt"
	"io/fs"
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

func makeBatchAssetItem(item MuseAssetBatchItem) MuseAssetItem {
	return MuseAssetItem{
		ID:       uuid.NewString(),
		Angle:    item.Angle,
		Variant:  item.Variant,
		State:    item.State,
		Version:  item.Version,
		Primary:  item.Primary,
		Rejected: item.Rejected,
		Lock:     item.Lock,
	}
}

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

// MuseFolderImage describes one image found in a picked folder or dropped file list before import.
type MuseFolderImage struct {
	Path       string           `json:"path"`
	LocalURL   string           `json:"localUrl,omitempty"`
	FileName   string           `json:"fileName"`
	RelPath    string           `json:"relPath,omitempty"`
	Size       int64            `json:"size"`
	PreviewURL string           `json:"previewUrl,omitempty"`
	Parsed     *ParsedAssetName `json:"parsed,omitempty"`
	Error      string           `json:"error,omitempty"`
}

// MuseAssetFolderScan is the naturally ordered image list of a folder or file selection.
type MuseAssetFolderScan struct {
	Canceled bool              `json:"canceled"`
	Dir      string            `json:"dir"`
	Images   []MuseFolderImage `json:"images"`
}

// MuseAssetBatchItem is one asset row sent to ImportAssetBatch.
type MuseAssetBatchItem struct {
	SourcePath string        `json:"sourcePath,omitempty"`
	SourceURL  string        `json:"sourceUrl,omitempty"`
	Project    string        `json:"project,omitempty"`
	Type       MuseAssetType `json:"type,omitempty"`
	Tag        string        `json:"tag"`
	Name       string        `json:"name"`
	Role       MuseRefRole   `json:"role"`
	Lock       string        `json:"lock"`
	Angle      string        `json:"angle,omitempty"`
	Variant    string        `json:"variant,omitempty"`
	State      string        `json:"state,omitempty"`
	Version    int           `json:"version,omitempty"`
	Primary    bool          `json:"primary,omitempty"`
	Rejected   bool          `json:"rejected,omitempty"`
}

// MuseAssetBatchRequest imports a list of asset images into the Asset Vault at once.
type MuseAssetBatchRequest struct {
	Items        []MuseAssetBatchItem   `json:"items"`
	Conflict     MuseAssetBatchConflict `json:"conflict"`
	MergeByAlias bool                   `json:"mergeByAlias,omitempty"`
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

// ScanAssetFolder reads the image files inside dir (and optionally subdirectories),
// sorted in natural filename order with convention auto-parsing and lightweight previews.
func (p *Provider) ScanAssetFolder(dir string, recursive bool) models.Response[MuseAssetFolderScan] {
	caps, _ := p.knownCapabilities()
	if caps == nil {
		return fail[MuseAssetFolderScan](errLimitsUnknown, models.CodeUnavailable)
	}
	var images []MuseFolderImage
	addEntry := func(fullPath, relPath, name string, info os.FileInfo) {
		ext := strings.ToLower(filepath.Ext(name))
		if _, ok := caps.imageMime(ext); !ok {
			return
		}
		item := MuseFolderImage{
			Path:     fullPath,
			FileName: name,
			RelPath:  relPath,
			Size:     info.Size(),
			Parsed:   ParseAssetFileName(name),
		}
		if info.Size() > caps.MaxFileBytes {
			item.Error = fmt.Sprintf("Lớn hơn %d MB (giới hạn Muse)", caps.MaxFileBytes>>20)
		}
		images = append(images, item)
	}

	if !recursive {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fail[MuseAssetFolderScan](fmt.Errorf("Không đọc được thư mục %s: %w", dir, err), models.CodeInvalidArgument)
		}
		images = make([]MuseFolderImage, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			fullPath := filepath.Join(dir, entry.Name())
			info, err := os.Stat(fullPath)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			addEntry(fullPath, entry.Name(), entry.Name(), info)
		}
	} else {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				if path != dir && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() {
				return nil
			}
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				rel = d.Name()
			}
			addEntry(path, filepath.ToSlash(rel), d.Name(), info)
			return nil
		})
		if err != nil {
			return fail[MuseAssetFolderScan](fmt.Errorf("Không đọc được thư mục %s: %w", dir, err), models.CodeInvalidArgument)
		}
	}

	slices.SortFunc(images, func(a, b MuseFolderImage) int {
		if c := naturalCompareFileName(a.RelPath, b.RelPath); c != 0 {
			return c
		}
		return naturalCompareFileName(a.FileName, b.FileName)
	})
	populateFolderPreviews(images)
	return models.SuccessResponse(MuseAssetFolderScan{Dir: dir, Images: images})
}

// ScanAssetPaths inspects a user-provided list of file paths (e.g. from drag-and-drop
// or multi-file picker) and returns natural-sorted scan rows with parsed convention hints.
func (p *Provider) ScanAssetPaths(paths []string) models.Response[MuseAssetFolderScan] {
	caps, _ := p.knownCapabilities()
	if caps == nil {
		return fail[MuseAssetFolderScan](errLimitsUnknown, models.CodeUnavailable)
	}
	seen := make(map[string]bool, len(paths))
	images := make([]MuseFolderImage, 0, len(paths))
	for _, raw := range paths {
		clean := filepath.Clean(strings.TrimSpace(raw))
		if clean == "" || seen[strings.ToLower(clean)] {
			continue
		}
		seen[strings.ToLower(clean)] = true
		info, err := os.Stat(clean)
		if err != nil || info.IsDir() || !info.Mode().IsRegular() {
			continue
		}
		name := filepath.Base(clean)
		ext := strings.ToLower(filepath.Ext(name))
		if _, ok := caps.imageMime(ext); !ok {
			continue
		}
		item := MuseFolderImage{
			Path:     clean,
			FileName: name,
			RelPath:  name,
			Size:     info.Size(),
			Parsed:   ParseAssetFileName(name),
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
	return models.SuccessResponse(MuseAssetFolderScan{Images: images})
}

func populateFolderPreviews(images []MuseFolderImage) {
	if len(images) == 0 {
		return
	}
	jobs := make(chan int, len(images))
	for i := range images {
		if images[i].Error == "" && images[i].Path != "" {
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

// ImportAssetBatch validates every row, copies accepted images (from disk paths
// or in-app /flow-media/ URLs) into the Asset Vault with canonical convention
// filenames, and saves all assets to the journal in one atomic write.
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

	type resolvedItem struct {
		item       MuseAssetBatchItem
		sourceAbs  string
		ext        string
		parsedProj string
	}
	normalized := make([]resolvedItem, len(req.Items))
	for i, item := range req.Items {
		tag, name, lock, err := normalizeAssetFields(item.Tag, item.Name, item.Lock)
		if err != nil {
			return fail[MuseAssetBatchResult](fmt.Errorf("Dòng %d (@%s): %w", i+1, item.Tag, err), models.CodeInvalidArgument)
		}
		if !item.Role.valid() {
			return fail[MuseAssetBatchResult](fmt.Errorf("Dòng %d (@%s): vai trò ảnh không hợp lệ.", i+1, tag), models.CodeInvalidArgument)
		}
		proj := normalizeProject(item.Project)
		assetType := item.Type
		if !assetType.valid() {
			assetType = typeForRole(item.Role)
		}

		srcAbs := strings.TrimSpace(item.SourcePath)
		if srcAbs == "" && strings.TrimSpace(item.SourceURL) != "" {
			resolved, pathErr := p.media.Path(item.SourceURL)
			if pathErr != nil {
				return fail[MuseAssetBatchResult](fmt.Errorf("Dòng %d (@%s): %w", i+1, tag, pathErr), models.CodeInvalidArgument)
			}
			srcAbs = resolved
		}
		if _, ext, _, chkErr := checkImageFile(caps, srcAbs); chkErr != nil {
			return fail[MuseAssetBatchResult](fmt.Errorf("Dòng %d (@%s): %w", i+1, tag, chkErr), models.CodeInvalidArgument)
		} else {
			item.Project = proj
			item.Type = assetType
			item.Tag = tag
			item.Name = name
			item.Lock = lock
			item.Angle = slugifyToken(item.Angle, false)
			item.Variant = slugifyToken(item.Variant, false)
			item.State = slugifyToken(item.State, false)
			if item.Version < 1 {
				item.Version = 1
			}
			normalized[i] = resolvedItem{item: item, sourceAbs: srcAbs, ext: ext, parsedProj: proj}
		}
	}

	p.assetMu.Lock()
	defer p.assetMu.Unlock()

	existingByKey := make(map[string]MuseAsset)
	taken := make(map[string]bool)
	for _, a := range p.assets.List() {
		key := assetScopeKey(a.Project, a.Tag)
		existingByKey[key] = a
		taken[key] = true
	}

	now := p.now().UTC()
	type plannedAsset struct {
		asset       MuseAsset
		sourceFiles []string // aligned with new/appended items at the end of asset.Items
		replaceAll  bool
		isUpdate    bool
	}
	var plan []plannedAsset
	planIdxByKey := make(map[string]int)
	skipped := 0

	for idx, entry := range normalized {
		item := entry.item
		key := assetScopeKey(item.Project, item.Tag)

		if req.MergeByAlias {
			if pIdx, ok := planIdxByKey[key]; ok {
				if strings.TrimSpace(plan[pIdx].asset.Name) == "" && item.Name != "" {
					plan[pIdx].asset.Name = item.Name
				}
				plan[pIdx].asset.Lock = combinedLock(plan[pIdx].asset.Lock, item.Lock)
				if !plan[pIdx].asset.Role.valid() && item.Role.valid() {
					plan[pIdx].asset.Role = item.Role
					plan[pIdx].asset.Type = item.Type
				}
				if item.Primary {
					for i := range plan[pIdx].asset.Items {
						plan[pIdx].asset.Items[i].Primary = false
					}
				}
				plan[pIdx].asset.Items = append(plan[pIdx].asset.Items, makeBatchAssetItem(item))
				plan[pIdx].sourceFiles = append(plan[pIdx].sourceFiles, entry.sourceAbs)
				continue
			}
			if old, exists := existingByKey[key]; exists {
				cloned := old
				cloned.Items = slices.Clone(old.Items)
				if strings.TrimSpace(cloned.Name) == "" && item.Name != "" {
					cloned.Name = item.Name
				}
				cloned.Lock = combinedLock(cloned.Lock, item.Lock)
				if !cloned.Role.valid() && item.Role.valid() {
					cloned.Role = item.Role
					cloned.Type = item.Type
				}
				if item.Primary {
					for i := range cloned.Items {
						cloned.Items[i].Primary = false
					}
				}
				cloned.Items = append(cloned.Items, makeBatchAssetItem(item))
				planIdxByKey[key] = len(plan)
				plan = append(plan, plannedAsset{
					asset:       cloned,
					sourceFiles: []string{entry.sourceAbs},
					isUpdate:    true,
				})
				continue
			}
			taken[key] = true
			planIdxByKey[key] = len(plan)
			plan = append(plan, plannedAsset{
				asset: MuseAsset{
					ID:        uuid.NewString(),
					Project:   item.Project,
					Type:      item.Type,
					Tag:       item.Tag,
					Name:      item.Name,
					Role:      item.Role,
					Lock:      item.Lock,
					Items:     []MuseAssetItem{makeBatchAssetItem(item)},
					CreatedAt: now.Add(time.Duration(idx) * time.Millisecond).Format(time.RFC3339),
				},
				sourceFiles: []string{entry.sourceAbs},
			})
			continue
		}

		switch conflict {
		case MuseConflictSkip:
			if taken[key] {
				skipped++
				continue
			}
			taken[key] = true
		case MuseConflictRename:
			if taken[key] {
				item.Tag = uniqueAssetTagInProject(item.Project, item.Tag, taken)
				key = assetScopeKey(item.Project, item.Tag)
			}
			taken[key] = true
		case MuseConflictOverwrite:
			taken[key] = true
		}

		newItem := makeBatchAssetItem(item)
		if pIdx, ok := planIdxByKey[key]; ok {
			plan[pIdx].asset.Project = item.Project
			plan[pIdx].asset.Type = item.Type
			plan[pIdx].asset.Tag = item.Tag
			plan[pIdx].asset.Name = item.Name
			plan[pIdx].asset.Role = item.Role
			plan[pIdx].asset.Lock = item.Lock
			plan[pIdx].asset.Items = []MuseAssetItem{newItem}
			plan[pIdx].sourceFiles = []string{entry.sourceAbs}
			plan[pIdx].replaceAll = true
			continue
		}
		if old, exists := existingByKey[key]; exists {
			planIdxByKey[key] = len(plan)
			plan = append(plan, plannedAsset{
				asset: MuseAsset{
					ID:        old.ID,
					Project:   item.Project,
					Type:      item.Type,
					Tag:       item.Tag,
					Name:      item.Name,
					Role:      item.Role,
					Lock:      item.Lock,
					Items:     []MuseAssetItem{newItem},
					CreatedAt: old.CreatedAt,
				},
				sourceFiles: []string{entry.sourceAbs},
				replaceAll:  true,
				isUpdate:    true,
			})
			continue
		}

		planIdxByKey[key] = len(plan)
		plan = append(plan, plannedAsset{
			asset: MuseAsset{
				ID:        uuid.NewString(),
				Project:   item.Project,
				Type:      item.Type,
				Tag:       item.Tag,
				Name:      item.Name,
				Role:      item.Role,
				Lock:      item.Lock,
				Items:     []MuseAssetItem{newItem},
				CreatedAt: now.Add(time.Duration(idx) * time.Millisecond).Format(time.RFC3339),
			},
			sourceFiles: []string{entry.sourceAbs},
		})
	}

	if len(plan) == 0 {
		return models.SuccessResponse(MuseAssetBatchResult{Skipped: skipped, SavedIDs: []string{}})
	}

	writtenVaultAbs := make([]string, 0, len(normalized))
	cleanupWritten := func() {
		for _, abs := range writtenVaultAbs {
			_ = os.Remove(abs)
		}
	}

	var adds, updates []MuseAsset
	var oldMediaURLs, oldVaultAbs []string
	savedIDs := make([]string, 0, len(plan))

	for i := range plan {
		entry := &plan[i]
		ensurePrimaryItem(entry.asset.Items)
		newStart := len(entry.asset.Items) - len(entry.sourceFiles)
		for k, srcAbs := range entry.sourceFiles {
			it := &entry.asset.Items[newStart+k]
			ext := strings.ToLower(filepath.Ext(srcAbs))
			if ext == "" {
				ext = ".png"
			}
			relPath, fileName, usedVer, pathErr := p.uniqueVaultRelPath(
				entry.asset.Project, entry.asset.Type, entry.asset.Tag,
				it.Angle, it.Variant, it.State, it.Version, it.Rejected, ext, "",
			)
			if pathErr != nil {
				cleanupWritten()
				return fail[MuseAssetBatchResult](pathErr, models.CodeInternal)
			}
			dstAbs, absErr := p.vaultFileAbs(relPath)
			if absErr != nil {
				cleanupWritten()
				return fail[MuseAssetBatchResult](absErr, models.CodeInternal)
			}
			if err := copyFileToFile(srcAbs, dstAbs); err != nil {
				cleanupWritten()
				return fail[MuseAssetBatchResult](fmt.Errorf("Không ghi được %s vào kho tư liệu: %w", fileName, err), models.CodeInternal)
			}
			writtenVaultAbs = append(writtenVaultAbs, dstAbs)
			it.Version = usedVer
			it.FileName = fileName
			it.VaultRelPath = relPath
			it.URL = media.VaultURL(relPath)
		}
		syncAssetImages(&entry.asset)
		savedIDs = append(savedIDs, entry.asset.ID)
		if entry.isUpdate {
			if entry.replaceAll {
				if prev, ok := p.assets.Get(entry.asset.ID); ok {
					oldMediaURLs = append(oldMediaURLs, prev.Images...)
					for _, prevIt := range prev.Items {
						if strings.HasPrefix(prevIt.URL, media.VaultRoute) {
							if abs, err := p.media.VaultPath(prevIt.URL); err == nil {
								oldVaultAbs = append(oldVaultAbs, abs)
							}
						}
					}
				}
			}
			updates = append(updates, entry.asset)
		} else {
			adds = append(adds, entry.asset)
		}
	}

	if err := p.assets.SaveBatch(adds, updates); err != nil {
		cleanupWritten()
		return fail[MuseAssetBatchResult](err, models.CodeInternal)
	}
	p.removeUnusedAssetMedia(oldMediaURLs)
	p.removeUnusedVaultFiles(oldVaultAbs)
	p.sweepAssetImages()
	return models.SuccessResponse(MuseAssetBatchResult{
		Added:    len(adds),
		Updated:  len(updates),
		Skipped:  skipped,
		SavedIDs: savedIDs,
	})
}

func uniqueAssetTagInProject(project, tag string, taken map[string]bool) string {
	runes := []rune(tag)
	for n := 2; ; n++ {
		suffix := fmt.Sprintf("_%d", n)
		suffixRunes := utf8.RuneCountInString(suffix)
		keep := min(len(runes), maxTagRunes-suffixRunes)
		candidate := string(runes[:keep]) + suffix
		if !taken[assetScopeKey(project, candidate)] {
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
