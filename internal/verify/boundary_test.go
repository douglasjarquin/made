package verify_test

import (
	"go/build"
	"testing"
)

func TestPackage_NeverImportsDaemonGateOrOrchestrator(t *testing.T) {
	pkg, err := build.ImportDir("../verify", 0)
	if err != nil {
		t.Fatalf("import ../verify: %v", err)
	}
	forbidden := map[string]bool{
		"github.com/douglasjarquin/made/internal/orchestrator": true,
		"github.com/douglasjarquin/made/internal/gitgate":      true,
		"github.com/douglasjarquin/made/internal/daemon":       true,
	}
	for _, imp := range pkg.Imports {
		if forbidden[imp] {
			t.Errorf("internal/verify must never import %q", imp)
		}
	}
}
