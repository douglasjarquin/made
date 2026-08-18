package github

import (
	"net/url"
	"strconv"
	"strings"
)

type CheckScope string

const (
	CheckScopeRequired CheckScope = "required"
	CheckScopeAll      CheckScope = "all"
)

func (s CheckScope) Valid() bool {
	return s == CheckScopeRequired || s == CheckScopeAll
}

type CheckResult struct {
	Name          string `json:"name"`
	State         string `json:"state"`
	Bucket        string `json:"bucket"`
	DetailsLink   string `json:"link"`
	WorkflowRunID string `json:"-"`
	ActionsBacked bool   `json:"-"`
	Rerunnable    bool   `json:"-"`
	Required      bool   `json:"-"`
	InScope       bool   `json:"-"`
}

type ChecksResult struct {
	Checks   []CheckResult
	ExitCode int
}

func enrichCheck(check *CheckResult, required, inScope bool) {
	check.Required = required
	check.InScope = inScope
	check.ActionsBacked, check.WorkflowRunID = workflowRunOwnership(check.DetailsLink)
	check.Rerunnable = check.ActionsBacked && check.WorkflowRunID != ""
}

func annotateRequired(checks, required []CheckResult) {
	for i := range checks {
		checks[i].InScope = true
		for _, requiredCheck := range required {
			if sameCheck(checks[i], requiredCheck) {
				checks[i].Required = true
				break
			}
		}
	}
}

func sameCheck(left, right CheckResult) bool {
	if left.Name != right.Name {
		return false
	}
	if left.DetailsLink == "" || right.DetailsLink == "" {
		return true
	}
	return left.DetailsLink == right.DetailsLink
}

func workflowRunOwnership(link string) (bool, string) {
	parsed, err := url.Parse(link)
	if err != nil {
		return false, ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return false, ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] != "actions" || parts[i+1] != "runs" {
			continue
		}
		if i+2 >= len(parts) {
			return true, ""
		}
		runID := parts[i+2]
		if _, err := strconv.ParseUint(runID, 10, 64); err != nil {
			return true, ""
		}
		return true, runID
	}
	return false, ""
}
