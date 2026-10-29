package media

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"SnakeMedia/internal/models"
)

// Reveal opens the system file manager with target selected: a stored file
// (its local URL) or a file saved under an allowed root, never anything else.
func (s *Store) Reveal(target string) models.Response[bool] {
	target = strings.TrimSpace(target)
	if target == "" {
		return models.ErrorResponse[bool]("Đường dẫn file không được để trống.", models.CodeInvalidArgument)
	}
	if strings.HasPrefix(target, Route) {
		if real, err := s.Path(target); err == nil {
			target = real
		}
	}
	checked, err := s.CheckFile(target)
	if err != nil {
		return models.ErrorResponse[bool](err.Error(), models.CodeInvalidArgument)
	}
	if _, err := os.Stat(checked); err != nil {
		return models.ErrorResponse[bool](fmt.Sprintf("File không tồn tại trên đĩa: %s", checked), models.CodeNotFound)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", "/select,", filepath.Clean(checked))
	case "darwin":
		cmd = exec.Command("open", "-R", checked)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(checked))
	}
	if err := cmd.Start(); err != nil {
		return models.ErrorResponse[bool](errors.Join(errors.New("Không mở được thư mục"), err).Error(), models.CodeInternal)
	}
	// Reap the process so its handle is released; its exit code says nothing
	// (explorer exits 1 even when it opened the folder).
	go func() { _ = cmd.Wait() }()
	return models.SuccessResponse(true)
}
