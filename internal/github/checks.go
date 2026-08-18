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

func enrichCheck(check *CheckResult, required, inScope bool, prURL string) {
	check.Required = required
	check.InScope = inScope
	check.ActionsBacked, check.WorkflowRunID = workflowRunOwnership(check.DetailsLink, prURL)
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

func workflowRunOwnership(link, prURL string) (bool, string) {
	prHost, prRepo, ok := repositoryPath(prURL)
	if !ok || prHost != "github.com" {
		return false, ""
	}
	linkHost, linkRepo, ok := repositoryPath(link)
	if !ok || linkHost != prHost || linkRepo != prRepo {
		return false, ""
	}

	parsed, err := url.Parse(link)
	if err != nil {
		return false, ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 5 || parts[2] != "actions" || parts[3] != "runs" {
		return false, ""
	}
	runID := parts[4]
	if _, err := strconv.ParseUint(runID, 10, 64); err != nil {
		return true, ""
	}
	return true, runID
}

func repositoryPath(raw string) (string, string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if parsed.Hostname() == "" || len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "www.github.com" {
		host = "github.com"
	}
	return host, strings.ToLower(strings.Join(parts[:2], "/")), true
}
