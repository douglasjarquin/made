package agenttest

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
		dir, err := os.MkdirTemp("", "fakeagent-bin-*")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(dir, "fakeagent")
		cmd := exec.Command("go", "build", "-o", binPath, "github.com/douglasjarquin/made/internal/agent/testdata/fakeagent")
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			buildErr = err
		}
	})
	if buildErr != nil {
		t.Fatalf("agenttest: build fakeagent: %v", buildErr)
	}
	return binPath
}
