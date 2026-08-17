package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/exec"
)

type Client struct {
	Dir      string
	Binary   string
	ExtraEnv []string
	Timeout  time.Duration
}

type AuthError struct {
	Detail string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("github: not authenticated: %s", e.Detail)
}

type CreatePROptions struct {
	Title string
	Body  string
	Base  string
	Head  string
}

type CheckResult struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Bucket string `json:"bucket"`
	Link   string `json:"link"`
	RunID  string `json:"-"`
}

type ChecksResult struct {
	Checks   []CheckResult
	ExitCode int
}

func (c *Client) AuthStatus(ctx context.Context) error {
	res, err := c.run(ctx, "auth", "status")
	if err != nil {
		return fmt.Errorf("github: run gh auth status: %w", err)
	}
	if res.ExitCode != 0 {
		return &AuthError{Detail: strings.TrimSpace(string(res.Stderr))}
	}
	return nil
}

func (c *Client) CreatePR(ctx context.Context, opts CreatePROptions) (string, error) {
	if err := c.AuthStatus(ctx); err != nil {
		return "", err
	}

	args := []string{"pr", "create", "--title", opts.Title, "--body", opts.Body}
	if opts.Base != "" {
		args = append(args, "--base", opts.Base)
	}
	if opts.Head != "" {
		args = append(args, "--head", opts.Head)
	}

	res, err := c.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("github: run gh pr create: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("github: gh pr create failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	return lastLine(res.Stdout), nil
}

func (c *Client) PRChecks(ctx context.Context, prURL string) (ChecksResult, error) {
	if err := c.AuthStatus(ctx); err != nil {
		return ChecksResult{}, err
	}

	res, err := c.run(ctx, "pr", "checks", prURL, "--json", "name,state,bucket,link")
	if err != nil {
		return ChecksResult{}, fmt.Errorf("github: run gh pr checks: %w", err)
	}
	if len(strings.TrimSpace(string(res.Stdout))) == 0 {
		return ChecksResult{}, fmt.Errorf("github: gh pr checks returned no JSON (exit %d): %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}

	var checks []CheckResult
	if err := json.Unmarshal(res.Stdout, &checks); err != nil {
		return ChecksResult{}, fmt.Errorf("github: parse gh pr checks output: %w: stdout=%s", err, res.Stdout)
	}
	for i := range checks {
		checks[i].RunID = workflowRunID(checks[i].Link)
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
		return "", fmt.Errorf("github: gh run view failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	return string(res.Stdout), nil
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
		return fmt.Errorf("github: gh run rerun failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func (c *Client) run(ctx context.Context, args ...string) (*exec.Result, error) {
	binary := c.Binary
	if binary == "" {
		binary = "gh"
	}
	env := c.ExtraEnv
	if env == nil {
		env = os.Environ()
	}
	return exec.Run(ctx, exec.Command{
		Name:    binary,
		Args:    args,
		Dir:     c.Dir,
		Env:     env,
		Timeout: c.Timeout,
	})
}

func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func validateWorkflowRunID(runID string) error {
	if _, err := strconv.ParseUint(runID, 10, 64); err != nil {
		return fmt.Errorf("github: invalid workflow run ID %q: %w", runID, err)
	}
	return nil
}

func workflowRunID(link string) string {
	parsed, err := url.Parse(link)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "runs" {
			if _, err := strconv.ParseUint(parts[i+1], 10, 64); err == nil {
				return parts[i+1]
			}
		}
	}
	return ""
}
