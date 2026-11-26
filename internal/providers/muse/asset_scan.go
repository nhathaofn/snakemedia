package muse

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"SnakeMedia/internal/models"
)

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
// sorted in natural filename order. Preview images are served on demand.
func (p *Provider) ScanAssetFolder(ctx context.Context, dir string, recursive bool) models.Response[MuseAssetFolderScan] {
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
			if ctx.Err() != nil {
				return cancelledAssetScan()
			}
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
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				if path == dir {
					return walkErr
				}
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
		if ctx.Err() != nil {
			return cancelledAssetScan()
		}
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
	return p.assetScanResult(ctx, dir, images, caps.MaxFileBytes)
}

// ScanAssetPaths inspects a user-provided list of file paths (e.g. from drag-and-drop
// or multi-file picker) and returns natural-sorted scan rows with parsed convention hints.
func (p *Provider) ScanAssetPaths(ctx context.Context, paths []string) models.Response[MuseAssetFolderScan] {
	caps, _ := p.knownCapabilities()
	if caps == nil {
		return fail[MuseAssetFolderScan](errLimitsUnknown, models.CodeUnavailable)
	}
	seen := make(map[string]bool, len(paths))
	images := make([]MuseFolderImage, 0, len(paths))
	for _, raw := range paths {
		if ctx.Err() != nil {
			return cancelledAssetScan()
		}
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
	return p.assetScanResult(ctx, "", images, caps.MaxFileBytes)
}

func cancelledAssetScan() models.Response[MuseAssetFolderScan] {
	return models.SuccessResponse(MuseAssetFolderScan{Canceled: true, Images: []MuseFolderImage{}})
}

func (p *Provider) assetScanResult(ctx context.Context, dir string, images []MuseFolderImage, maxFileBytes int64) models.Response[MuseAssetFolderScan] {
	if ctx.Err() != nil {
		return cancelledAssetScan()
	}
	paths := make([]string, len(images))
	for i := range images {
		if images[i].Error == "" {
			paths[i] = images[i].Path
		}
	}
	prefix := p.media.ImportPreviewPrefix(paths, maxFileBytes)
	for i, path := range paths {
		if path != "" {
			images[i].PreviewURL = prefix + strconv.Itoa(i)
		}
	}
	return models.SuccessResponse(MuseAssetFolderScan{Dir: dir, Images: images})
}
