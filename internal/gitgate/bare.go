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
	return EnableAdvertisePushOptions(path)
}

// EnableAdvertisePushOptions sets receive.advertisePushOptions on an
// existing bare gate repo, idempotently. InitBare always calls this on a
// fresh gate; it is also exported so a gate initialized before agent
// auto-resolve existed can self-heal on next use (project: agent
// auto-resolve, decision D5) - without it, `git push -o agent=<kind>`
// against an older gate hard-fails with "the receiving end does not
// support push options" rather than being silently ignored.
func EnableAdvertisePushOptions(path string) error {
	cmd := exec.Command("git", "-C", path, "config", "receive.advertisePushOptions", "true")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gitgate: enable advertisePushOptions on %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}
