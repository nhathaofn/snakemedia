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
	p.vaultMu.Lock()
	defer p.vaultMu.Unlock()
	p.assetMu.Lock()
	defer p.assetMu.Unlock()

	oldDir := p.vaultDir()
	oldClean, err := resolveCleanDir(oldDir)
	if err != nil {
		return fail[string](fmt.Errorf("Thư mục kho tư liệu hiện tại không hợp lệ: %w", err), models.CodeInternal)
	}
	targetClean, err := resolveCleanDir(targetDir)
	if err != nil {
		return fail[string](fmt.Errorf("Thư mục kho tư liệu đích không hợp lệ: %w", err), models.CodeInvalidArgument)
	}
	sameDir := sameDirPath(oldClean, targetClean)
	if migrate && !sameDir && (dirContains(oldClean, targetClean) || dirContains(targetClean, oldClean)) {
		return fail[string](errors.New("Thư mục kho tư liệu mới không được nằm lồng với thư mục hiện tại."), models.CodeConflict)
	}

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
				srcAbs, srcPathErr := resolveRelUnderDir(oldClean, rel)
				dstAbs, dstPathErr := resolveRelUnderDir(targetClean, rel)
				if srcPathErr != nil || dstPathErr != nil {
					continue
				}
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
				srcAbs, err := resolveRelUnderDir(oldClean, rel)
				if err != nil {
					return fail[string](fmt.Errorf("Đường dẫn ảnh nguồn không hợp lệ (%s): %w", rel, err), models.CodeInvalidArgument)
				}
				dstAbs, err := resolveRelUnderDir(targetClean, rel)
				if err != nil {
					return fail[string](fmt.Errorf("Đường dẫn ảnh đích không hợp lệ (%s): %w", rel, err), models.CodeInvalidArgument)
				}
				planKey := strings.ToLower(filepath.Clean(dstAbs))
				if planned[planKey] {
					continue
				}
				srcInfo, err := os.Stat(srcAbs)
				if err != nil {
					return fail[string](fmt.Errorf("Không đọc được ảnh nguồn %s: %w", rel, err), models.CodeUnavailable)
				}
				planned[planKey] = true
				pair := movedPair{src: srcAbs, dst: dstAbs}
				if info, err := os.Lstat(dstAbs); err == nil {
					if dstInfo, statErr := os.Stat(dstAbs); statErr == nil && os.SameFile(srcInfo, dstInfo) {
						// Source and destination resolve to the same physical file (e.g. symlink/junction):
						// keep it untouched and never remove src.
						continue
					}
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
					removeMigratedSource(pair.src, pair.dst)
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
		removeMigratedSource(m.src, m.dst)
	}
	return models.SuccessResponse(targetDir)
}

func removeMigratedSource(src, dst string) {
	srcInfo, srcErr := os.Stat(src)
	dstInfo, dstErr := os.Stat(dst)
	if srcErr == nil && dstErr == nil && os.SameFile(srcInfo, dstInfo) {
		return
	}
	if err := os.Remove(src); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Get().Warn("cannot remove old vault file after migration", "path", src, "error", err)
	}
}

func resolveCleanDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	existing, rest := abs, ""
	for {
		resolved, err := filepath.EvalSymlinks(existing)
		if err == nil {
			return filepath.Join(resolved, rest), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
}

func dirContains(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func resolveRelUnderDir(cleanRoot, rel string) (string, error) {
	if rel == "" || strings.ContainsRune(rel, 0) || strings.Contains(rel, `\`) || strings.Contains(rel, ":") {
		return "", errors.New("tệp nằm ngoài thư mục kho tư liệu")
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("tệp nằm ngoài thư mục kho tư liệu")
		}
	}
	candidate := filepath.Join(append([]string{cleanRoot}, parts...)...)
	cleanTarget, err := resolveCleanDir(candidate)
	if err != nil || !dirContains(cleanRoot, cleanTarget) || sameDirPath(cleanRoot, cleanTarget) {
		return "", errors.New("tệp nằm ngoài thư mục kho tư liệu")
	}
	return cleanTarget, nil
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
