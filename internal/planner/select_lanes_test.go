package planner

import (
	"testing"

	"github.com/douglasjarquin/made/internal/config"
)

func laneNamed(t *testing.T, decisions []LaneDecision, name string) LaneDecision {
	t.Helper()
	for _, d := range decisions {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no lane decision named %q in %+v", name, decisions)
	return LaneDecision{}
}

func TestSelectLanes_NoLanesConfiguredFallsBackToDefault(t *testing.T) {
	decisions, err := SelectLanes(nil, []string{"main.go"})
	if err != nil {
		t.Fatalf("SelectLanes: %v", err)
	}
	if len(decisions) != 1 || decisions[0].Name != defaultLaneName || decisions[0].Action != "run" {
		t.Fatalf("expected a single running default lane, got %+v", decisions)
	}
}

func TestSelectLanes_PathSelectsMatchingLaneOnly(t *testing.T) {
	lanes := map[string]config.Lane{
		"go":   {Paths: []string{"**/*.go"}, RequiredBeforePush: true},
		"docs": {Paths: []string{"**/*.md"}, RequiredBeforePush: true},
	}
	decisions, err := SelectLanes(lanes, []string{"main.go"})
	if err != nil {
		t.Fatalf("SelectLanes: %v", err)
	}
	if got := laneNamed(t, decisions, "go"); got.Action != "run" {
		t.Fatalf("expected go lane to run, got %+v", got)
	}
	if got := laneNamed(t, decisions, "docs"); got.Action != "skip" {
		t.Fatalf("expected docs lane to skip, got %+v", got)
	}
}

func TestSelectLanes_PathCanSelectMultipleLanes(t *testing.T) {
	lanes := map[string]config.Lane{
		"go":        {Paths: []string{"**/*.go"}, RequiredBeforePush: true},
		"workflows": {Paths: []string{".github/workflows/**"}, RequiredBeforePush: true},
	}
	decisions, err := SelectLanes(lanes, []string{"main.go", ".github/workflows/ci.yml"})
	if err != nil {
		t.Fatalf("SelectLanes: %v", err)
	}
	if got := laneNamed(t, decisions, "go"); got.Action != "run" {
		t.Fatalf("expected go lane to run, got %+v", got)
	}
	if got := laneNamed(t, decisions, "workflows"); got.Action != "run" {
		t.Fatalf("expected workflows lane to run, got %+v", got)
	}
}

func TestSelectLanes_UnknownPathFailsOpenToAllRequiredLanes(t *testing.T) {
	lanes := map[string]config.Lane{
		"go":   {Paths: []string{"**/*.go"}, RequiredBeforePush: true},
		"docs": {Paths: []string{"**/*.md"}, RequiredBeforePush: true},
	}
	decisions, err := SelectLanes(lanes, []string{"weird.unknownext"})
	if err != nil {
		t.Fatalf("SelectLanes: %v", err)
	}
	for _, name := range []string{"go", "docs"} {
		got := laneNamed(t, decisions, name)
		if got.Action != "run" {
			t.Fatalf("expected lane %q to fail open to run on an unclassified path, got %+v", name, got)
		}
	}
}

func TestSelectLanes_UnknownPathDoesNotForceNonRequiredLane(t *testing.T) {
	lanes := map[string]config.Lane{
		"optional": {Paths: []string{"**/*.md"}, RequiredBeforePush: false},
	}
	decisions, err := SelectLanes(lanes, []string{"weird.unknownext"})
	if err != nil {
		t.Fatalf("SelectLanes: %v", err)
	}
	if got := laneNamed(t, decisions, "optional"); got.Action != "skip" {
		t.Fatalf("expected a non-required lane to stay skipped despite an unclassified path, got %+v", got)
	}
}

func TestSelectLanes_DependsOnPatternAlsoSelectsLane(t *testing.T) {
	lanes := map[string]config.Lane{
		"go": {Paths: []string{"**/*.go"}, DependsOn: []string{".github/workflows/ci.yml"}, RequiredBeforePush: true},
	}
	decisions, err := SelectLanes(lanes, []string{".github/workflows/ci.yml"})
	if err != nil {
		t.Fatalf("SelectLanes: %v", err)
	}
	if got := laneNamed(t, decisions, "go"); got.Action != "run" {
		t.Fatalf("expected a depends_on match to select the lane, got %+v", got)
	}
}

func TestSelectLanes_MalformedPatternPropagatesError(t *testing.T) {
	lanes := map[string]config.Lane{
		"broken": {Paths: []string{"[unterminated"}},
	}
	if _, err := SelectLanes(lanes, []string{"anything"}); err == nil {
		t.Fatal("expected an error for a malformed lane pattern")
	}
}

func TestSelectLanes_IsDeterministicOrder(t *testing.T) {
	lanes := map[string]config.Lane{
		"zzz": {Paths: []string{"**/*.zzz"}, RequiredBeforePush: true},
		"aaa": {Paths: []string{"**/*.aaa"}, RequiredBeforePush: true},
	}
	first, err := SelectLanes(lanes, []string{"file.aaa"})
	if err != nil {
		t.Fatalf("SelectLanes: %v", err)
	}
	if first[0].Name != "aaa" || first[1].Name != "zzz" {
		t.Fatalf("expected lane decisions sorted by name, got %+v", first)
	}
}
