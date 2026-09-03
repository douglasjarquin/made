package verify_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/verify"
)

const testConfigCursorModel = `version: 1
review:
  executors:
    cursor:
      model: "claude-opus-5[effort=high]"
`

func TestPrepare_DefaultsRequestedModelFromConfiguredCursorExecutor(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigCursorModel)
	out, err := verify.Prepare(context.Background(), verify.PrepareParams{
		WorkDir:  dir,
		BaseRef:  "origin/main",
		Executor: "cursor",
		Output:   filepath.Join(t.TempDir(), "request.json"),
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if want := "claude-opus-5[effort=high]"; out.Request.RequestedModel != want {
		t.Fatalf("RequestedModel = %q, want %q from review.executors.cursor.model", out.Request.RequestedModel, want)
	}
}

func TestPrepare_ExplicitRequestedModelOverridesConfig(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigCursorModel)
	out, err := verify.Prepare(context.Background(), verify.PrepareParams{
		WorkDir:        dir,
		BaseRef:        "origin/main",
		Executor:       "cursor",
		RequestedModel: "explicit-override",
		Output:         filepath.Join(t.TempDir(), "request.json"),
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if out.Request.RequestedModel != "explicit-override" {
		t.Fatalf("RequestedModel = %q, want explicit --requested-model to override config", out.Request.RequestedModel)
	}
}

func TestPrepare_NoConfiguredModelLeavesRequestedModelEmpty(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigNoAgent)
	out, err := verify.Prepare(context.Background(), verify.PrepareParams{
		WorkDir:  dir,
		BaseRef:  "origin/main",
		Executor: "cursor",
		Output:   filepath.Join(t.TempDir(), "request.json"),
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if out.Request.RequestedModel != "" {
		t.Fatalf("RequestedModel = %q, want empty when review.executors.cursor.model is unset", out.Request.RequestedModel)
	}
}
