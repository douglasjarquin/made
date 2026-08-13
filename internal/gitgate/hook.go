package gitgate

import (
	"fmt"
	"os"
	"path/filepath"
)

func InstallHooks(repoPath, madeBinaryPath, madeHome string) error {
	hooksDir := filepath.Join(repoPath, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("gitgate: create hooks dir: %w", err)
	}

	preReceivePath := filepath.Join(hooksDir, "pre-receive")
	if err := os.WriteFile(preReceivePath, []byte(preReceiveScript(repoPath, madeBinaryPath, madeHome)), 0o700); err != nil {
		return fmt.Errorf("gitgate: write pre-receive hook: %w", err)
	}

	postReceivePath := filepath.Join(hooksDir, "post-receive")
	if err := os.WriteFile(postReceivePath, []byte(postReceiveScript(repoPath, madeBinaryPath, madeHome)), 0o700); err != nil {
		return fmt.Errorf("gitgate: write post-receive hook: %w", err)
	}

	return nil
}

func preReceiveScript(repoPath, madeBinaryPath, madeHome string) string {
	return `#!/bin/sh
export MADE_HOME="` + madeHome + `"
"` + madeBinaryPath + `" gate admit-push --gate "` + repoPath + `"
exit $?
`
}

func postReceiveScript(repoPath, madeBinaryPath, madeHome string) string {
	return `#!/bin/sh
export MADE_HOME="` + madeHome + `"
while read old_sha new_sha refname; do
  "` + madeBinaryPath + `" gate notify-push --gate "` + repoPath + `" --old "$old_sha" --new "$new_sha" --ref "$refname"
done
exit 0
`
}
