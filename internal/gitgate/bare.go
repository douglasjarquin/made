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
	// Override safe.bareRepository for this bare repo's initialization to permit access.
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=safe.bareRepository",
		"GIT_CONFIG_VALUE_0=all",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gitgate: git init --bare %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}
