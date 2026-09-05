package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

const (
	quotaAxiBinary          = "quota-axi"
	quotaAxiProbeTimeout    = 5 * time.Second
	quotaExhaustedThreshold = 1.0
	quotaScopeAllModels     = "all_models"
)

// QuotaSignal is the one piece of information a resolver needs from
// quota-axi: is this candidate currently blocked by quota, and if so when
// does that clear. A nil *QuotaSignal (returned alongside a nil error) means
// no usable signal was available - never treated as exhausted.
type QuotaSignal struct {
	Exhausted bool
	ResetsAt  *time.Time
}

type quotaAxiReport struct {
	Providers []struct {
		Windows []struct {
			PercentRemaining *float64 `json:"percentRemaining"`
			ResetsAt         string   `json:"resetsAt"`
		} `json:"windows"`
		QuotaSemantics struct {
			Status                string `json:"status"`
			EffectiveAvailability []struct {
				Scope                     string  `json:"scope"`
				Status                    string  `json:"status"`
				EffectivePercentRemaining float64 `json:"effectivePercentRemaining"`
				Runway                    struct {
					ProjectedExhaustedAt string `json:"projectedExhaustedAt"`
				} `json:"runway"`
			} `json:"effectiveAvailability"`
		} `json:"quotaSemantics"`
	} `json:"providers"`
}

// probeQuota shells out to the optional quota-axi CLI for kind and reports
// whether it currently has usable quota. quota-axi is a probed, optional
// dependency (project: agent auto-resolve) - its absence, a nonzero exit, an
// unparseable response, or a report this parser doesn't recognize as
// "known" all mean "no signal," never "exhausted": only an explicit,
// parsed, sub-threshold remaining percentage skips a candidate.
//
// quota-axi's own quotaSemantics.effectiveAvailability pre-computes the
// bound across every window that actually limits a scope (e.g. a 5-hour
// session window together with a 7-day weekly window), so this probe reads
// that field rather than re-deriving the bound from raw windows itself.
// Made never pins a specific sub-model per provider, so the "all_models"
// scope is the one that applies; a report predating that field (older
// quota-axi schema) falls back to a conservative scan of raw windows for
// any percentRemaining below the same threshold.
func probeQuota(ctx context.Context, kind Kind) (*QuotaSignal, error) {
	binary, err := exec.LookPath(quotaAxiBinary)
	if err != nil {
		return nil, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, quotaAxiProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, binary, "--provider", string(kind), "--json", "--full", "--no-credential-refresh")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, nil
	}
	return parseQuotaReport(stdout.Bytes())
}

func parseQuotaReport(data []byte) (*QuotaSignal, error) {
	var report quotaAxiReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, nil
	}
	for _, provider := range report.Providers {
		if provider.QuotaSemantics.Status == "known" {
			for _, avail := range provider.QuotaSemantics.EffectiveAvailability {
				if avail.Scope != quotaScopeAllModels || avail.Status != "known" {
					continue
				}
				if avail.EffectivePercentRemaining >= quotaExhaustedThreshold {
					return nil, nil
				}
				return &QuotaSignal{Exhausted: true, ResetsAt: parseResetsAt(avail.Runway.ProjectedExhaustedAt)}, nil
			}
			return nil, nil
		}
		for _, window := range provider.Windows {
			if window.PercentRemaining != nil && *window.PercentRemaining < quotaExhaustedThreshold {
				return &QuotaSignal{Exhausted: true, ResetsAt: parseResetsAt(window.ResetsAt)}, nil
			}
		}
	}
	return nil, nil
}

func parseResetsAt(raw string) *time.Time {
	if raw == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return &t
}
