package verify_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitAt(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newTestRepo creates a git repo with a base commit and an input commit on
// top of it, and a local refs/remotes/origin/main ref pinned at the base
// commit - simulating a normal "origin/main" local ref without any network
// fetch, matching made verify's "local base resolution only" contract.
func newTestRepo(t *testing.T, configPath, configContent string) (dir, baseSHA, inputSHA string) {
	t.Helper()
	dir = t.TempDir()

	gitAt(t, dir, "init", "-b", "main")
	gitAt(t, dir, "config", "user.email", "test@test.local")
	gitAt(t, dir, "config", "user.name", "test")
	gitAt(t, dir, "config", "commit.gpgsign", "false")

	if configPath != "" {
		writeTestFile(t, filepath.Join(dir, configPath), configContent)
	}
	writeTestFile(t, filepath.Join(dir, "README.md"), "# test\n")
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", "initial")
	baseSHA = gitAt(t, dir, "rev-parse", "HEAD")

	gitAt(t, dir, "update-ref", "refs/remotes/origin/main", baseSHA)

	writeTestFile(t, filepath.Join(dir, "hello.go"), "package main\n")
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", "add hello.go")
	inputSHA = gitAt(t, dir, "rev-parse", "HEAD")

	return dir, baseSHA, inputSHA
}

const testConfigNoAgent = `version: 1
commands:
  test: "true"
  lint: "true"
`

const testConfigReviewRequired = `version: 1
review:
  required: true
commands:
  test: "true"
  lint: "true"
`

const testConfigFailingTest = `version: 1
commands:
  test: "false"
`

const testConfigLaneGo = `version: 1
validation:
  lanes:
    go:
      paths: ["**/*.go"]
      full: ["echo go-full"]
      required_before_push: true
`
