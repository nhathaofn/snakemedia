package muse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
	"SnakeMedia/pkg/utils"
	"github.com/google/uuid"
)

// assetFileChanges keeps old paths valid until the asset index is committed.
type assetFileChanges struct {
	created, replaced, temporaryMedia []string
}

func (c assetFileChanges) rollback() {
	for _, path := range c.created {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Get().Warn("cannot roll back new vault file", "path", path, "error", err)
		}
	}
}

func (p *Provider) finishAssetFiles(changes []assetFileChanges) {
	var temporary, replaced []string
	for _, change := range changes {
		temporary = append(temporary, change.temporaryMedia...)
		replaced = append(replaced, change.replaced...)
	}
	p.removeUnusedAssetMedia(temporary)
	p.removeUnusedVaultFiles(replaced)
	p.sweepAssetImages()
}

// materializeAssetItems publishes copies under canonical paths. The caller
// commits the new index or rolls back the new copies. Existing files remain
// available throughout, including to readers of the previous index.
func (p *Provider) materializeAssetItems(a *MuseAsset, old *MuseAsset) (changes assetFileChanges, err error) {
	var staged assetFileChanges
	defer func() {
		if err != nil {
			staged.rollback()
		}
	}()

	items := slices.Clone(a.Items)
	if len(items) == 0 {
		oldByURL := make(map[string]MuseAssetItem)
		if old != nil {
			for _, item := range old.Items {
				oldByURL[item.URL] = item
			}
		}
		for _, url := range a.Images {
			item, ok := oldByURL[url]
			if !ok {
				item = MuseAssetItem{URL: url, Version: 1}
			}
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return changes, errors.New("Mỗi tư liệu cần ít nhất 1 ảnh.")
	}

	ensurePrimaryItem(items)
	kept := make(map[string]bool, len(items))
	for i := range items {
		item := &items[i]
		if item.ID == "" {
			item.ID = uuid.NewString()
		}
		item.Angle = slugifyToken(item.Angle, false)
		item.Variant = slugifyToken(item.Variant, false)
		item.State = slugifyToken(item.State, false)
		item.Lock = strings.TrimSpace(item.Lock)
		item.Version = max(item.Version, 1)
		if item.Rejected {
			item.Primary = false
		}

		var src, existing string
		switch {
		case strings.HasPrefix(item.URL, media.Route):
			src, err = p.media.Path(item.URL)
		case strings.HasPrefix(item.URL, media.VaultRoute):
			src, err = p.media.VaultPath(item.URL)
			existing = src
		default:
			return changes, errors.New("Ảnh tư liệu phải nằm trong kho hoặc được chọn từ ứng dụng.")
		}
		if err != nil {
			return changes, err
		}
		if info, statErr := os.Stat(src); statErr != nil || !info.Mode().IsRegular() {
			return changes, fmt.Errorf("Không đọc được ảnh %s.", filepath.Base(src))
		}
		ext := strings.ToLower(filepath.Ext(src))
		if ext == "" {
			ext = ".png"
		}
		rel, name, version, err := p.uniqueVaultRelPath(
			a.Project, a.Type, a.Tag, item.Angle, item.Variant, item.State, item.Version, item.Rejected, ext, existing,
		)
		if err != nil {
			return changes, err
		}
		dst, err := p.vaultFileAbs(rel)
		if err != nil {
			return changes, err
		}
		if !strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) {
			if err := copyVaultFile(src, dst, utils.PublishFile); err != nil {
				return changes, fmt.Errorf("Không ghi được ảnh %s vào kho tư liệu: %w", filepath.Base(src), err)
			}
			staged.created = append(staged.created, dst)
		}
		if strings.HasPrefix(item.URL, media.Route+assetPrefix) {
			staged.temporaryMedia = append(staged.temporaryMedia, item.URL)
		}
		item.Version, item.FileName, item.VaultRelPath, item.URL = version, name, rel, media.VaultURL(rel)
		kept[strings.ToLower(filepath.Clean(dst))] = true
	}

	if old != nil {
		for _, prev := range old.Items {
			if strings.HasPrefix(prev.URL, media.VaultRoute) {
				if abs, err := p.media.VaultPath(prev.URL); err == nil && !kept[strings.ToLower(filepath.Clean(abs))] {
					staged.replaced = append(staged.replaced, abs)
				}
			}
			if strings.HasPrefix(prev.URL, media.Route+assetPrefix) {
				staged.temporaryMedia = append(staged.temporaryMedia, prev.URL)
			}
		}
	}
	a.Items = items
	syncAssetImages(a)
	return staged, nil
}
