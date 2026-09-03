package cursor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/safegit"
	"github.com/douglasjarquin/made/internal/verify"
)

type CheckStatus string

const (
	StatusOK      CheckStatus = "ok"
	StatusWarn    CheckStatus = "warn"
	StatusFail    CheckStatus = "fail"
	StatusSkipped CheckStatus = "skipped"
)

type DoctorCheck struct {
	Name   string      `json:"name"`
	Status CheckStatus `json:"status"`
	Detail string      `json:"detail,omitempty"`
}

type DoctorReport struct {
	Healthy bool          `json:"healthy"`
	Checks  []DoctorCheck `json:"checks"`
}

type DoctorParams struct {
	Root    string
	BaseRef string
}

func Doctor(ctx context.Context, p DoctorParams) DoctorReport {
	var checks []DoctorCheck
	healthy := true
	record := func(c DoctorCheck) {
		checks = append(checks, c)
		if c.Status == StatusFail {
			healthy = false
		}
	}

	record(DoctorCheck{Name: "made_binary", Status: StatusOK, Detail: "version=" + managed.MadeVersion})

	loc, locErr := config.Locate(p.Root)
	var cfg config.Config
	haveConfig := false
	if locErr != nil {
		record(DoctorCheck{Name: "config", Status: StatusFail, Detail: locErr.Error()})
	} else if loc.Layout == config.LayoutAbsent {
		record(DoctorCheck{Name: "config", Status: StatusFail, Detail: "no .made.yaml or .made/config.yaml found"})
	} else {
		data, err := os.ReadFile(loc.Path)
		if err != nil {
			record(DoctorCheck{Name: "config", Status: StatusFail, Detail: err.Error()})
		} else if parsed, err := config.ParseBytes(data); err != nil {
			record(DoctorCheck{Name: "config", Status: StatusFail, Detail: err.Error()})
		} else {
			cfg = parsed
			haveConfig = true
			record(DoctorCheck{Name: "config", Status: StatusOK, Detail: fmt.Sprintf("path=%s layout=%s", loc.Path, loc.Layout)})
		}
	}

	model := cfg.Review.Executors.Cursor.Model
	switch {
	case !haveConfig:
		record(DoctorCheck{Name: "cursor_executor", Status: StatusSkipped, Detail: "no valid configuration"})
	case model != "":
		record(DoctorCheck{Name: "cursor_executor", Status: StatusOK, Detail: "configured"})
	case cfg.Review.Required:
		record(DoctorCheck{Name: "cursor_executor", Status: StatusWarn, Detail: "review.required is true but review.executors.cursor.model is unset; the reviewer subagent will be skipped for this executor"})
	default:
		record(DoctorCheck{Name: "cursor_executor", Status: StatusOK, Detail: "not_configured"})
	}

	if model != "" {
		if err := cfg.Validate(); err != nil {
			record(DoctorCheck{Name: "cursor_model", Status: StatusFail, Detail: err.Error()})
		} else {
			record(DoctorCheck{Name: "cursor_model", Status: StatusOK, Detail: model})
		}
	} else {
		record(DoctorCheck{Name: "cursor_model", Status: StatusSkipped, Detail: "not configured"})
	}

	if haveConfig {
		guideRoot := managed.TrustedGuideRoot(loc.Path)
		if _, err := managed.ResolveTrustedGuides(guideRoot, cfg.Review.Guides); err != nil {
			record(DoctorCheck{Name: "review_guides", Status: StatusFail, Detail: err.Error()})
		} else {
			record(DoctorCheck{Name: "review_guides", Status: StatusOK, Detail: fmt.Sprintf("n=%d", len(cfg.Review.Guides))})
		}
	} else {
		record(DoctorCheck{Name: "review_guides", Status: StatusSkipped})
	}

	if haveConfig {
		drift, err := Check(p.Root, cfg)
		if err != nil {
			record(DoctorCheck{Name: "projections", Status: StatusFail, Detail: err.Error()})
		} else if len(drift) > 0 {
			record(DoctorCheck{Name: "projections", Status: StatusFail, Detail: fmt.Sprintf("%d projection(s) drifted; run `made cursor sync`", len(drift))})
		} else {
			record(DoctorCheck{Name: "projections", Status: StatusOK, Detail: "current"})
		}
	} else {
		record(DoctorCheck{Name: "projections", Status: StatusSkipped})
	}

	if model != "" {
		if reviewer, err := ReviewerMarkdown(model, cfg.Review.Guides); err != nil {
			record(DoctorCheck{Name: "reviewer_schema", Status: StatusFail, Detail: err.Error()})
		} else if !hasExpectedReviewerFrontmatter(reviewer, model) {
			record(DoctorCheck{Name: "reviewer_schema", Status: StatusFail, Detail: "reviewer frontmatter is missing readonly:true/is_background:false or the configured model"})
		} else {
			record(DoctorCheck{Name: "reviewer_schema", Status: StatusOK})
		}
	} else {
		record(DoctorCheck{Name: "reviewer_schema", Status: StatusSkipped})
	}

	record(DoctorCheck{Name: "verify_external", Status: StatusOK, Detail: "made verify prepare/complete supports --executor with an external review result"})

	if p.BaseRef == "" {
		record(DoctorCheck{Name: "base_ref", Status: StatusSkipped, Detail: "no --base-ref given"})
	} else if _, err := safegit.Output(ctx, safegit.Command{WorktreePath: p.Root, Args: []string{"rev-parse", "--verify", p.BaseRef + "^{commit}"}}); err != nil {
		// A shallow or limited Cloud VM clone may not have every ref
		// available locally; that is a real, survivable condition, so this
		// check never gates Doctor's overall healthy.
		record(DoctorCheck{Name: "base_ref", Status: StatusWarn, Detail: fmt.Sprintf("%q was not found locally; made never fetches automatically", p.BaseRef)})
	} else {
		record(DoctorCheck{Name: "base_ref", Status: StatusOK})
	}

	stateDir := verify.StateRoot(p.Root)
	if err := checkWritable(stateDir); err != nil {
		record(DoctorCheck{Name: "temp_paths", Status: StatusFail, Detail: err.Error()})
	} else {
		record(DoctorCheck{Name: "temp_paths", Status: StatusOK, Detail: stateDir})
	}

	return DoctorReport{Healthy: healthy, Checks: checks}
}

func hasExpectedReviewerFrontmatter(markdown, model string) bool {
	required := []string{
		"model: " + model,
		"readonly: true",
		"is_background: false",
	}
	for _, want := range required {
		if !strings.Contains(markdown, want) {
			return false
		}
	}
	return true
}

func checkWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	probe := filepath.Join(dir, ".made-cursor-doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return err
	}
	return os.Remove(probe)
}
