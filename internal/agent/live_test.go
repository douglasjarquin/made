package agent_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/agent"
)

// TestLive_SpawnEveryHarness drives each real reviewer CLI on PATH against a
// throwaway repository. It is opt-in because it needs installed, authenticated
// CLIs and spends real model tokens:
//
//	MADE_AGENT_LIVE_TEST=codex,claude,cursor,grok go test ./internal/agent -run TestLive_ -v
func TestLive_SpawnEveryHarness(t *testing.T) {
	selected := strings.TrimSpace(os.Getenv("MADE_AGENT_LIVE_TEST"))
	if selected == "" {
		t.Skip("set MADE_AGENT_LIVE_TEST to a comma-separated list of agent kinds to run live spawns")
	}
	worktree := agentWorktree(t)
	if err := os.WriteFile(worktree+"/hello.go", []byte("package hello\n\nfunc Hello() string { return \"hi\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAgent(t, worktree, "add", ".")
	gitAgent(t, worktree, "commit", "-q", "-m", "add hello")
	for name := range strings.SplitSeq(selected, ",") {
		kind, err := agent.ParseKind(strings.TrimSpace(name))
		if err != nil {
			t.Fatal(err)
		}
		t.Run(string(kind), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			result, err := agent.SpawnWithEvidence(ctx, kind, agent.SpawnParams{
				WorktreePath: worktree,
				Task:         "Review the most recent commit in this repository. It adds hello.go. Report zero findings unless you see a real defect. Return only the structured object matching the supplied output schema.",
				Timeout:      5 * time.Minute,
			})
			if err != nil {
				t.Fatalf("live spawn %s: %v", kind, err)
			}
			t.Logf("%s response: %s", kind, result.Response)
		})
	}
}
