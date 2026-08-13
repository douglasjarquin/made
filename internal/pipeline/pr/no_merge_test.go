package pr_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/github"
)

var forbiddenMergeMethodNames = []string{
	"Merge",
	"MergePR",
	"MergePullRequest",
	"AutoMerge",
	"EnableAutoMerge",
}

func TestGitHubClientExposesNoMergeCapableMethod(t *testing.T) {
	clientType := reflect.TypeOf((*github.Client)(nil))
	for _, name := range forbiddenMergeMethodNames {
		if _, ok := clientType.MethodByName(name); ok {
			t.Fatalf("internal/github.Client must not expose a %s method: made's PR stage must never be able to merge a pull request", name)
		}
	}
}

var suspiciousMergeSubstrings = []string{
	`"merge"`,
	`"Merge`,
	"--merge",
	"--auto-merge",
	"mergepr",
	"pr merge",
	".merge(",
}

func TestPackageSourceContainsNoMergeInvocation(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file's own path")
	}
	dir := filepath.Dir(thisFile)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		lower := strings.ToLower(string(data))
		for _, s := range suspiciousMergeSubstrings {
			if strings.Contains(lower, strings.ToLower(s)) {
				t.Fatalf("file %s contains suspicious merge-invocation substring %q; made's PR stage must never call a merge operation", name, s)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test .go files found in internal/pipeline/pr; expected pr.go to exist")
	}
}
