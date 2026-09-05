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

// postReceiveScript, per updated ref, reads GIT_PUSH_OPTION_COUNT/
// GIT_PUSH_OPTION_<n> (populated by git only when the pushing client used
// `-o` and the server advertises the push-options capability, per
// EnableAdvertisePushOptions in bare.go) looking for one shaped
// "agent=<value>", and forwards it as --agent-preference (project: agent
// auto-resolve). The value is only ever used as a literal shell-variable
// expansion (an exec argument to made itself) - it is never eval'd or
// otherwise interpolated into a command string, so an adversarial pushed
// value cannot escape into shell execution.
func postReceiveScript(repoPath, madeBinaryPath, madeHome string) string {
	return `#!/bin/sh
export MADE_HOME="` + madeHome + `"
agent_pref=""
if [ -n "$GIT_PUSH_OPTION_COUNT" ]; then
  i=0
  while [ "$i" -lt "$GIT_PUSH_OPTION_COUNT" ]; do
    eval "opt=\$GIT_PUSH_OPTION_$i"
    case "$opt" in
      agent=*) agent_pref="${opt#agent=}" ;;
    esac
    i=$((i + 1))
  done
fi
while read old_sha new_sha refname; do
  if [ -n "$agent_pref" ]; then
    "` + madeBinaryPath + `" gate notify-push --gate "` + repoPath + `" --old "$old_sha" --new "$new_sha" --ref "$refname" --agent-preference "$agent_pref"
  else
    "` + madeBinaryPath + `" gate notify-push --gate "` + repoPath + `" --old "$old_sha" --new "$new_sha" --ref "$refname"
  fi
done
exit 0
`
}
