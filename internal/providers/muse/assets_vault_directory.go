package muse

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/models"
	"SnakeMedia/pkg/utils"
)

// GetAssetVaultDirectory returns the active Asset Vault path (configured folder or AppData default).
func (p *Provider) GetAssetVaultDirectory() models.Response[string] {
	dir := p.vaultDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail[string](fmt.Errorf("Không tạo được thư mục kho tư liệu: %w", err), models.CodeInternal)
	}
	return models.SuccessResponse(dir)
}

// SetAssetVaultDirectory updates config.Muse.AssetVaultDir to dir (or clears it when empty
// to reset back to AppData) and optionally migrates existing vault files into the target folder.
func (p *Provider) SetAssetVaultDirectory(newDir string, migrate bool) models.Response[string] {
	newDir = strings.TrimSpace(newDir)
	if newDir != "" {
		abs, err := filepath.Abs(newDir)
		if err != nil {
			return fail[string](fmt.Errorf("Đường dẫn kho tư liệu không hợp lệ: %w", err), models.CodeInvalidArgument)
		}
		newDir = abs
	}
	targetDir := newDir
	if targetDir == "" {
		targetDir = p.defaultVaultDir
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fail[string](fmt.Errorf("Không tạo được thư mục kho tư liệu: %w", err), models.CodeInternal)
	}
	p.assetMu.Lock()
	defer p.assetMu.Unlock()

	oldDir := p.vaultDir()
	sameDir := sameDirPath(oldDir, targetDir)

	type movedPair struct {
		src string
		dst string
	}
	var moved []movedPair
	committed := false
	defer func() {
		if !committed {
			for _, m := range moved {
				_ = os.Remove(m.dst)
			}
		}
	}()

	if !migrate && !sameDir {
		missingAtDest := 0
		for _, a := range p.assets.List() {
			for _, it := range a.Items {
				if !strings.HasPrefix(it.URL, media.VaultRoute) {
					continue
				}
				rel := it.VaultRelPath
				if rel == "" {
					rel = strings.TrimPrefix(it.URL, media.VaultRoute)
				}
				srcAbs := filepath.Join(oldDir, filepath.FromSlash(rel))
				dstAbs := filepath.Join(targetDir, filepath.FromSlash(rel))
				if _, dstErr := os.Stat(dstAbs); dstErr != nil {
					if _, srcErr := os.Stat(srcAbs); srcErr == nil {
						missingAtDest++
					}
				}
			}
		}
		if missingAtDest > 0 {
			logger.Get().Warn("muse vault directory switched without migration; existing vault files remain in old folder",
				"missingFiles", missingAtDest, "oldDir", oldDir, "newDir", targetDir)
		}
	}

	if migrate && !sameDir {
		planned := make(map[string]bool)
		var copies []movedPair
		var matching []movedPair
		for _, a := range p.assets.List() {
			for _, it := range a.Items {
				if !strings.HasPrefix(it.URL, media.VaultRoute) {
					continue
				}
				rel := it.VaultRelPath
				if rel == "" {
					rel = strings.TrimPrefix(it.URL, media.VaultRoute)
				}
				srcAbs := filepath.Join(oldDir, filepath.FromSlash(rel))
				dstAbs := filepath.Join(targetDir, filepath.FromSlash(rel))
				if planned[dstAbs] {
					continue
				}
				if _, err := os.Stat(srcAbs); err != nil {
					return fail[string](fmt.Errorf("Không đọc được ảnh nguồn %s: %w", rel, err), models.CodeUnavailable)
				}
				planned[dstAbs] = true
				pair := movedPair{src: srcAbs, dst: dstAbs}
				if info, err := os.Lstat(dstAbs); err == nil {
					if info.Mode().IsRegular() {
						before, srcErr := utils.FileDigest(srcAbs)
						after, dstErr := utils.FileDigest(dstAbs)
						if srcErr == nil && dstErr == nil && before == after {
							matching = append(matching, pair)
							continue
						}
					}
					return fail[string](fmt.Errorf("Thư mục đích đã có tệp %s. Hãy chọn thư mục khác hoặc xử lý tệp trùng.", rel), models.CodeConflict)
				} else if !errors.Is(err, os.ErrNotExist) {
					return fail[string](err, models.CodeInternal)
				}
				copies = append(copies, pair)
			}
		}
		for _, copy := range copies {
			if err := copyVaultFile(copy.src, copy.dst, utils.PublishFile); err != nil {
				return fail[string](fmt.Errorf("Không chuyển được ảnh %s sang thư mục mới: %w", filepath.Base(copy.src), err), models.CodeInternal)
			}
			moved = append(moved, copy)
		}
		defer func() {
			if committed {
				for _, pair := range matching {
					_ = os.Remove(pair.src)
				}
			}
		}()
	}

	if p.cfg != nil {
		cfg := p.cfg.Get()
		cfg.Muse.AssetVaultDir = newDir
		if _, err := p.cfg.Update(cfg); err != nil {
			return fail[string](fmt.Errorf("Không lưu được cài đặt thư mục kho: %w", err), models.CodeInternal)
		}
	}

	committed = true
	p.media.AllowRoot(targetDir)
	for _, m := range moved {
		_ = os.Remove(m.src)
	}
	return models.SuccessResponse(targetDir)
}

// OpenAssetVaultFolder opens the active Asset Vault folder in the system file explorer.
func (p *Provider) OpenAssetVaultFolder() models.Response[bool] {
	dir := p.vaultDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	if err := utils.OpenFolder(dir); err != nil {
		return fail[bool](err, models.CodeInternal)
	}
	return models.SuccessResponse(true)
}
