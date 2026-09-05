package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAgentResolution_MarshalsSelectedKind(t *testing.T) {
	claude := KindClaude
	res := AgentResolution{
		Selected: &claude,
		Attempts: []CandidateAttempt{{Kind: KindCodex, Reason: ReasonMissing}},
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	out := string(data)
	if !strings.Contains(out, `"selected":"claude"`) {
		t.Errorf("json = %s, want it to contain \"selected\":\"claude\"", out)
	}
	if !strings.Contains(out, `"reason":"missing"`) {
		t.Errorf("json = %s, want it to contain \"reason\":\"missing\"", out)
	}
	if res.AllExhausted() {
		t.Errorf("AllExhausted() = true, want false (Selected is set)")
	}
}

func TestAgentResolution_ExhaustedOmitsSelected(t *testing.T) {
	resetsAt := time.Date(2026, 9, 10, 6, 0, 0, 0, time.UTC)
	res := AgentResolution{
		Attempts: []CandidateAttempt{
			{Kind: KindCodex, Reason: ReasonMissing},
			{Kind: KindClaude, Reason: ReasonQuotaExhausted, QuotaResetsAt: &resetsAt},
		},
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	out := string(data)
	if strings.Contains(out, `"selected"`) {
		t.Errorf("json = %s, want no \"selected\" key when Selected is nil", out)
	}
	if !strings.Contains(out, `"quota_resets_at"`) {
		t.Errorf("json = %s, want \"quota_resets_at\" present for the quota_exhausted attempt", out)
	}
	if !res.AllExhausted() {
		t.Errorf("AllExhausted() = false, want true (Selected is nil)")
	}
}
