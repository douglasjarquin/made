package agent

import (
	"context"
	"os/exec"
	"time"
)

// authProbeTimeout bounds each auth-status/quota probe (decision D8).
const authProbeTimeout = 5 * time.Second

// quotaProbe is a seam over probeQuota: resolve_test.go reassigns it to
// inject a canned QuotaSignal without needing a real quota-axi binary on
// PATH, since quota-axi's own JSON parsing is already covered independently
// by quotaaxi_test.go's golden fixtures.
var quotaProbe = probeQuota

// Resolve probes candidates in order - presence (LookPath), then auth
// (codex/claude only, decision D2), then quota (decision D10) - and returns
// the first one that passes all applicable steps, or, if none do, every
// candidate attempted and why. Probes run uncontained (decision D8): they
// are cheap, read-only status checks against the trusted host CLI's own
// auth/quota state, never touching candidate/pushed worktree content, so
// the containment threat model a real review spawn needs does not apply
// here.
func Resolve(ctx context.Context, candidates []Kind) AgentResolution {
	var attempts []CandidateAttempt
	for _, kind := range candidates {
		if _, err := exec.LookPath(kind.BinaryName()); err != nil {
			attempts = append(attempts, CandidateAttempt{Kind: kind, Reason: ReasonMissing})
			continue
		}
		if !authProbe(ctx, kind) {
			attempts = append(attempts, CandidateAttempt{Kind: kind, Reason: ReasonUnauthenticated})
			continue
		}
		if signal, _ := quotaProbe(ctx, kind); signal != nil && signal.Exhausted {
			attempts = append(attempts, CandidateAttempt{Kind: kind, Reason: ReasonQuotaExhausted, QuotaResetsAt: signal.ResetsAt})
			continue
		}
		selected := kind
		return AgentResolution{Selected: &selected, Attempts: attempts}
	}
	return AgentResolution{Attempts: attempts}
}

// authProbe reports whether kind is authenticated. cursor/grok have no
// confirmed cheap auth-status command anywhere in this repo or verified
// live (decision D2), so presence is the best available signal for them -
// they always pass this step, exactly like the brief requires ("do not
// invent an auth check you haven't verified").
func authProbe(ctx context.Context, kind Kind) bool {
	var args []string
	switch kind {
	case KindCodex:
		args = []string{"login", "status"}
	case KindClaude:
		args = []string{"auth", "status"}
	default:
		return true
	}
	probeCtx, cancel := context.WithTimeout(ctx, authProbeTimeout)
	defer cancel()
	return exec.CommandContext(probeCtx, kind.BinaryName(), args...).Run() == nil
}
