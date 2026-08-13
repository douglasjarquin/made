package gitgate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func InitBare(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("gitgate: bare repo path must not be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("gitgate: create parent dir for %s: %w", path, err)
	}
	cmd := exec.Command("git", "init", "--bare", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gitgate: git init --bare %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}
