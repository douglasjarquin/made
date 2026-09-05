package config

import (
	"reflect"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
)

func TestConfig_AgentIsPinned(t *testing.T) {
	cases := []struct {
		agent string
		want  bool
	}{
		{"", false},
		{"auto", false},
		{"claude", true},
		{"codex", true},
	}
	for _, tc := range cases {
		cfg := Config{Agent: tc.agent}
		if got := cfg.AgentIsPinned(); got != tc.want {
			t.Errorf("Config{Agent: %q}.AgentIsPinned() = %v, want %v", tc.agent, got, tc.want)
		}
	}
}

func TestConfig_AgentCandidates_PinnedIgnoresAgentsEntirely(t *testing.T) {
	cfg := Config{Agent: "claude", Agents: []string{"codex", "grok"}}
	if !cfg.AgentIsPinned() {
		t.Fatalf("expected pinned config")
	}
	kind, err := cfg.AgentKind()
	if err != nil || kind != agent.KindClaude {
		t.Fatalf("AgentKind() = (%v, %v), want (claude, nil)", kind, err)
	}
}

func TestConfig_AgentCandidates_AutoWithAgentsUsesConfiguredOrder(t *testing.T) {
	cfg := Config{Agent: "auto", Agents: []string{"claude", "codex"}}
	got := cfg.AgentCandidates()
	want := []agent.Kind{agent.KindClaude, agent.KindCodex}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AgentCandidates() = %v, want %v", got, want)
	}
}

func TestConfig_AgentCandidates_EmptyAgentWithAgentsUsesConfiguredOrder(t *testing.T) {
	cfg := Config{Agent: "", Agents: []string{"grok", "cursor"}}
	got := cfg.AgentCandidates()
	want := []agent.Kind{agent.KindGrok, agent.KindCursor}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AgentCandidates() = %v, want %v", got, want)
	}
}

func TestConfig_AgentCandidates_AutoWithEmptyAgentsUsesSupportedKindsDefaultOrder(t *testing.T) {
	cfg := Config{Agent: "auto"}
	got := cfg.AgentCandidates()
	want := agent.SupportedKinds()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AgentCandidates() = %v, want %v (SupportedKinds() default order)", got, want)
	}
}

func TestConfig_Validate_ReviewRequiredPassesWithAutoAndAgentsList(t *testing.T) {
	cfg := Config{Review: Review{Required: true}, Agent: "auto", Agents: []string{"claude", "codex"}}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil (auto+agents: is always a valid selection mechanism)", err)
	}
}

func TestConfig_Validate_ReviewRequiredPassesWithEmptyAgentAndEmptyAgents(t *testing.T) {
	cfg := Config{Review: Review{Required: true}}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil (empty agent+agents falls back to SupportedKinds())", err)
	}
}

func TestConfig_Validate_StillRejectsExplicitInvalidAgent(t *testing.T) {
	cfg := Config{Review: Review{Required: true}, Agent: "gpt4"}
	if err := cfg.Validate(); err == nil {
		t.Errorf("Validate() = nil, want error for unrecognized pinned agent %q", "gpt4")
	}
}

func TestConfig_Validate_StillRejectsInvalidAgentsEntry(t *testing.T) {
	cfg := Config{Agent: "auto", Agents: []string{"claude", "gpt4"}}
	if err := cfg.Validate(); err == nil {
		t.Errorf("Validate() = nil, want error for unrecognized agents[1] value %q", "gpt4")
	}
}
