package ci

import (
	"fmt"
	"sort"
	"strings"

	"github.com/douglasjarquin/made/internal/github"
)

const maxFailureEvidenceBytes = 256 * 1024

type FailureEvidence struct {
	CheckNames    []string
	State         string
	Bucket        string
	DetailsLink   string
	WorkflowRunID string
	Excerpt       string
}

func terminalFailures(checks []github.CheckResult) (pending bool, failures []github.CheckResult) {
	for _, check := range checks {
		if !check.InScope {
			continue
		}
		if checkPending(check) {
			pending = true
			continue
		}
		if !checkSuccessful(check) {
			failures = append(failures, check)
		}
	}
	sort.Slice(failures, func(i, j int) bool {
		if failures[i].Name != failures[j].Name {
			return failures[i].Name < failures[j].Name
		}
		if failures[i].WorkflowRunID != failures[j].WorkflowRunID {
			return failures[i].WorkflowRunID < failures[j].WorkflowRunID
		}
		return failures[i].DetailsLink < failures[j].DetailsLink
	})
	return pending, failures
}

func checkPending(check github.CheckResult) bool {
	state := strings.ToUpper(strings.TrimSpace(check.State))
	bucket := strings.ToLower(strings.TrimSpace(check.Bucket))
	if bucket == "pending" {
		return true
	}
	switch state {
	case "PENDING", "QUEUED", "IN_PROGRESS", "WAITING", "EXPECTED":
		return true
	default:
		return false
	}
}

func checkSuccessful(check github.CheckResult) bool {
	bucket := strings.ToLower(strings.TrimSpace(check.Bucket))
	if bucket == "fail" || bucket == "cancel" {
		return false
	}
	if bucket == "pass" || bucket == "skipping" || bucket == "neutral" {
		return true
	}
	switch strings.ToUpper(strings.TrimSpace(check.State)) {
	case "SUCCESS", "COMPLETED", "SKIPPED", "NEUTRAL":
		return true
	default:
		return false
	}
}

func rerunnableRunIDs(checks []github.CheckResult) []string {
	seen := make(map[string]struct{}, len(checks))
	ids := make([]string, 0, len(checks))
	for _, check := range checks {
		if !check.Rerunnable || check.WorkflowRunID == "" {
			continue
		}
		if _, ok := seen[check.WorkflowRunID]; ok {
			continue
		}
		seen[check.WorkflowRunID] = struct{}{}
		ids = append(ids, check.WorkflowRunID)
	}
	sort.Strings(ids)
	return ids
}

func collectFailureEvidence(checks []github.CheckResult, logs map[string]string) []FailureEvidence {
	result := make([]FailureEvidence, 0, len(checks))
	indexes := make(map[string]int, len(checks))
	for _, check := range checks {
		if check.Rerunnable && check.WorkflowRunID != "" {
			key := "run:" + check.WorkflowRunID
			if index, ok := indexes[key]; ok {
				result[index].CheckNames = appendUnique(result[index].CheckNames, check.Name)
				continue
			}
			indexes[key] = len(result)
			result = append(result, FailureEvidence{
				CheckNames:    []string{check.Name},
				State:         check.State,
				Bucket:        check.Bucket,
				DetailsLink:   check.DetailsLink,
				WorkflowRunID: check.WorkflowRunID,
				Excerpt:       logs[check.WorkflowRunID],
			})
			continue
		}
		result = append(result, FailureEvidence{
			CheckNames:  []string{check.Name},
			State:       check.State,
			Bucket:      check.Bucket,
			DetailsLink: check.DetailsLink,
		})
	}
	return result
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func formatFailureMessage(prURL string, rounds, budget int, evidence []FailureEvidence) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "checks failed for %s after %d rerun round(s) (budget %d)", prURL, rounds, budget)
	for _, item := range evidence {
		fmt.Fprintf(&builder, "\n- %s [state=%s bucket=%s]", strings.Join(item.CheckNames, ", "), item.State, item.Bucket)
		if item.WorkflowRunID != "" {
			fmt.Fprintf(&builder, " Actions run %s", item.WorkflowRunID)
		}
		if item.DetailsLink != "" {
			fmt.Fprintf(&builder, " (%s)", item.DetailsLink)
		}
		if item.Excerpt != "" {
			fmt.Fprintf(&builder, ": %s", item.Excerpt)
		}
		if builder.Len() >= maxFailureEvidenceBytes {
			break
		}
	}
	return boundText(builder.String(), maxFailureEvidenceBytes)
}

func boundText(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	marker := "\n[truncated]\n"
	if len(marker) >= maxBytes {
		return marker[:maxBytes]
	}
	return value[:maxBytes-len(marker)] + marker
}
