package agent_test

import (
	"context"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/agent/agenttest"
)

func TestResolve_SkipsMissingSelectsNext(t *testing.T) {
	agenttest.BuildFleet(t, map[agent.Kind]agenttest.FleetOptions{
		agent.KindClaude: {AuthExitCode: 0},
	})

	res := agent.Resolve(context.Background(), []agent.Kind{agent.KindCodex, agent.KindClaude})

	if res.Selected == nil || *res.Selected != agent.KindClaude {
		t.Fatalf("Resolve().Selected = %v, want claude", res.Selected)
	}
	if len(res.Attempts) != 1 || res.Attempts[0].Kind != agent.KindCodex || res.Attempts[0].Reason != agent.ReasonMissing {
		t.Errorf("Resolve().Attempts = %+v, want one missing attempt for codex", res.Attempts)
	}
}

func TestResolve_SkipsUnauthenticatedSelectsNext(t *testing.T) {
	agenttest.BuildFleet(t, map[agent.Kind]agenttest.FleetOptions{
		agent.KindClaude: {AuthExitCode: 1},
		agent.KindCodex:  {AuthExitCode: 0},
	})

	res := agent.Resolve(context.Background(), []agent.Kind{agent.KindClaude, agent.KindCodex})

	if res.Selected == nil || *res.Selected != agent.KindCodex {
		t.Fatalf("Resolve().Selected = %v, want codex", res.Selected)
	}
	if len(res.Attempts) != 1 || res.Attempts[0].Kind != agent.KindClaude || res.Attempts[0].Reason != agent.ReasonUnauthenticated {
		t.Errorf("Resolve().Attempts = %+v, want one unauthenticated attempt for claude", res.Attempts)
	}
}

func TestResolve_CursorPresenceOnlyNoAuthProbeAttempted(t *testing.T) {
	agenttest.BuildFleet(t, map[agent.Kind]agenttest.FleetOptions{
		agent.KindCursor: {},
	})

	res := agent.Resolve(context.Background(), []agent.Kind{agent.KindCursor})

	if res.Selected == nil || *res.Selected != agent.KindCursor {
		t.Fatalf("Resolve().Selected = %v, want cursor (presence-only, decision D2)", res.Selected)
	}
	if len(res.Attempts) != 0 {
		t.Errorf("Resolve().Attempts = %+v, want none", res.Attempts)
	}
}

func TestResolve_CursorMissingRecordsMissing(t *testing.T) {
	agenttest.BuildFleet(t, map[agent.Kind]agenttest.FleetOptions{})

	res := agent.Resolve(context.Background(), []agent.Kind{agent.KindCursor})

	if res.Selected != nil {
		t.Fatalf("Resolve().Selected = %v, want nil", res.Selected)
	}
	if len(res.Attempts) != 1 || res.Attempts[0].Kind != agent.KindCursor || res.Attempts[0].Reason != agent.ReasonMissing {
		t.Errorf("Resolve().Attempts = %+v, want one missing attempt for cursor", res.Attempts)
	}
}

func TestResolve_AllCandidatesExhaustedMixedReasons(t *testing.T) {
	agenttest.BuildFleet(t, map[agent.Kind]agenttest.FleetOptions{
		agent.KindClaude: {AuthExitCode: 1},
	})

	res := agent.Resolve(context.Background(), []agent.Kind{agent.KindCodex, agent.KindClaude})

	if !res.AllExhausted() {
		t.Fatalf("Resolve().AllExhausted() = false, want true")
	}
	if len(res.Attempts) != 2 {
		t.Fatalf("Resolve().Attempts = %+v, want 2 entries", res.Attempts)
	}
	if res.Attempts[0].Kind != agent.KindCodex || res.Attempts[0].Reason != agent.ReasonMissing {
		t.Errorf("Attempts[0] = %+v, want codex/missing", res.Attempts[0])
	}
	if res.Attempts[1].Kind != agent.KindClaude || res.Attempts[1].Reason != agent.ReasonUnauthenticated {
		t.Errorf("Attempts[1] = %+v, want claude/unauthenticated", res.Attempts[1])
	}
}
