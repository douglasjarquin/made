package githubtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

func Build(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "fakegh-bin-*")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(dir, "fakegh")
		cmd := exec.Command("go", "build", "-o", binPath, "github.com/douglasjarquin/made/internal/github/testdata/fakegh")
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			buildErr = err
		}
	})
	if buildErr != nil {
		t.Fatalf("githubtest: build fakegh: %v", buildErr)
	}
	return binPath
}
