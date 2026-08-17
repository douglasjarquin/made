package rebase_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/pipeline/rebase"
)

func TestRun_DoesNotClassifyRebaseFailureAsConflictWithoutUnmergedPaths(t *testing.T) {
	worktree := t.TempDir()
	if err := os.Mkdir(filepath.Join(worktree, ".git"), 0o700); err != nil {
		t.Fatalf("make fake git dir: %v", err)
	}
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	script := "#!/bin/sh\nset -eu\ncd \"$2\"\nshift 2\ncase \"$*\" in\n  *'rev-parse --git-dir'*) printf '.git\\n' ;;\n  *'rebase --abort'*) rm -rf .git/rebase-merge ;;\n  *'diff --name-only --diff-filter=U'*) : ;;\n  *'rebase upstream'*) mkdir -p .git/rebase-merge; exit 1 ;;\n  *) exit 1 ;;\nesac\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	result, err := rebase.Run(worktree, "upstream")
	if err == nil {
		if !result.OK && len(result.ConflictingFiles) == 0 {
			t.Fatalf("rebase failure was labeled conflict without unmerged paths: %+v", result)
		}
		t.Fatalf("rebase failure without unmerged paths returned a normal conflict result: %+v", result)
	}
}
