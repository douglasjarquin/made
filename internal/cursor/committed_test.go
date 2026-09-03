package cursor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/cursor"
)

func TestCommittedCursorProjectionsMatchGenerator(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	loc, err := config.Locate(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(loc.Path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	drift, err := cursor.Check(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Fatalf("committed .cursor/ projections drifted from the generator; run `made cursor sync`: %+v", drift)
	}
}
