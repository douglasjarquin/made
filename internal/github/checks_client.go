package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const maxCheckLogBytes = 64 * 1024

func (c *Client) PRChecks(ctx context.Context, prURL string, scope CheckScope) (ChecksResult, error) {
	if strings.TrimSpace(prURL) == "" {
		return ChecksResult{}, fmt.Errorf("github: pull request URL is required for checks")
	}
	if !scope.Valid() {
		return ChecksResult{}, fmt.Errorf("github: unsupported check scope %q", scope)
	}
	if err := c.AuthStatus(ctx); err != nil {
		return ChecksResult{}, err
	}

	checks, err := c.prChecks(ctx, prURL, scope == CheckScopeRequired)
	if err != nil {
		return ChecksResult{}, err
	}
	if scope == CheckScopeRequired {
		for i := range checks.Checks {
			enrichCheck(&checks.Checks[i], true, true)
		}
		return checks, nil
	}

	required, err := c.prChecks(ctx, prURL, true)
	if err != nil {
		return ChecksResult{}, err
	}
	annotateRequired(checks.Checks, required.Checks)
	for i := range checks.Checks {
		enrichCheck(&checks.Checks[i], checks.Checks[i].Required, true)
	}
	return checks, nil
}

func (c *Client) prChecks(ctx context.Context, prURL string, required bool) (ChecksResult, error) {
	args := []string{"pr", "checks", prURL}
	if required {
		args = append(args, "--required")
	}
	args = append(args, "--json", "name,state,bucket,link")
	res, err := c.run(ctx, args...)
	if err != nil {
		return ChecksResult{}, fmt.Errorf("github: run gh pr checks: %w", err)
	}
	if isRateLimitDetail(string(res.Stderr)) {
		return ChecksResult{}, &RateLimitError{Operation: "gh pr checks", Detail: strings.TrimSpace(string(res.Stderr))}
	}
	if res.ExitCode != 0 && strings.TrimSpace(string(res.Stderr)) != "" {
		return ChecksResult{}, commandFailure("gh pr checks", res)
	}
	if len(strings.TrimSpace(string(res.Stdout))) == 0 {
		return ChecksResult{}, fmt.Errorf("github: gh pr checks returned no JSON (exit %d): %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}

	var checks []CheckResult
	if err := json.Unmarshal(res.Stdout, &checks); err != nil {
		return ChecksResult{}, fmt.Errorf("github: parse gh pr checks output: %w: stdout=%s", err, res.Stdout)
	}
	if res.ExitCode == 0 {
		for _, check := range checks {
			bucket := strings.ToLower(strings.TrimSpace(check.Bucket))
			if bucket != "pass" && bucket != "skipping" && bucket != "neutral" {
				return ChecksResult{}, fmt.Errorf("github: gh pr checks exit 0 with non-success bucket %q for %q", check.Bucket, check.Name)
			}
		}
	}
	return ChecksResult{Checks: checks, ExitCode: res.ExitCode}, nil
}

func (c *Client) CheckLogs(ctx context.Context, runID string) (string, error) {
	if err := validateWorkflowRunID(runID); err != nil {
		return "", err
	}
	if err := c.AuthStatus(ctx); err != nil {
		return "", err
	}

	res, err := c.run(ctx, "run", "view", runID, "--log")
	if err != nil {
		return "", fmt.Errorf("github: run gh run view: %w", err)
	}
	if res.ExitCode != 0 {
		return "", commandFailure("gh run view", res)
	}
	output := res.Stdout
	if len(output) > maxCheckLogBytes {
		output = append(append([]byte(nil), output[:maxCheckLogBytes]...), []byte("\n[truncated]\n")...)
	}
	return string(output), nil
}

func (c *Client) RerunCheck(ctx context.Context, runID string) error {
	if err := validateWorkflowRunID(runID); err != nil {
		return err
	}
	if err := c.AuthStatus(ctx); err != nil {
		return err
	}

	res, err := c.run(ctx, "run", "rerun", runID, "--failed")
	if err != nil {
		return fmt.Errorf("github: run gh run rerun: %w", err)
	}
	if res.ExitCode != 0 {
		return commandFailure("gh run rerun", res)
	}
	return nil
}
