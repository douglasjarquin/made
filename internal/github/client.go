package github

import (
	"context"
	"encoding/json"
	"fmt"
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

const maxCheckLogBytes = 64 * 1024

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

type Check struct {
	Name          string `json:"name"`
	Status        string `json:"state"`
	Conclusion    string `json:"conclusion"`
	WorkflowRunID string `json:"workflowRunId"`
	DetailsURL    string `json:"detailsUrl"`
}

func (c *Check) UnmarshalJSON(data []byte) error {
	var wire struct {
		Name          string          `json:"name"`
		Status        string          `json:"state"`
		Conclusion    string          `json:"conclusion"`
		WorkflowRunID json.RawMessage `json:"workflowRunId"`
		DetailsURL    string          `json:"detailsUrl"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	var runID string
	if len(wire.WorkflowRunID) > 0 && string(wire.WorkflowRunID) != "null" {
		if err := json.Unmarshal(wire.WorkflowRunID, &runID); err != nil {
			var numeric json.Number
			if err := json.Unmarshal(wire.WorkflowRunID, &numeric); err != nil {
				return fmt.Errorf("github: parse workflow run ID: %w", err)
			}
			runID = numeric.String()
		}
	}
	*c = Check{Name: wire.Name, Status: wire.Status, Conclusion: wire.Conclusion, WorkflowRunID: runID, DetailsURL: wire.DetailsURL}
	return nil
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
	if existing, err := c.findOpenPR(ctx, opts); err != nil {
		return "", err
	} else if existing != "" {
		return existing, nil
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
	url := lastLine(res.Stdout)
	if url == "" {
		return "", fmt.Errorf("github: gh pr create returned an empty URL")
	}
	return url, nil
}

func (c *Client) findOpenPR(ctx context.Context, opts CreatePROptions) (string, error) {
	args := []string{"pr", "list", "--state", "open", "--base", opts.Base, "--head", opts.Head, "--json", "url"}
	res, err := c.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("github: run gh pr list: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("github: gh pr list failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	var entries []struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(res.Stdout, &entries); err != nil {
		return "", fmt.Errorf("github: parse gh pr list output: %w", err)
	}
	if len(entries) == 0 {
		return "", nil
	}
	return entries[0].URL, nil
}

func (c *Client) MergeableState(ctx context.Context, prURL string) (string, error) {
	if err := c.AuthStatus(ctx); err != nil {
		return "", err
	}

	res, err := c.run(ctx, "pr", "view", prURL, "--json", "mergeStateStatus")
	if err != nil {
		return "", fmt.Errorf("github: run gh pr view: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("github: gh pr view failed: %s", strings.TrimSpace(string(res.Stderr)))
	}

	var payload struct {
		MergeStateStatus string `json:"mergeStateStatus"`
	}
	if err := json.Unmarshal(res.Stdout, &payload); err != nil {
		return "", fmt.Errorf("github: parse gh pr view output: %w: stdout=%s", err, res.Stdout)
	}
	return payload.MergeStateStatus, nil
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
		return fmt.Errorf("github: gh run rerun failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

func (c *Client) Checks(ctx context.Context, prURL string) ([]Check, error) {
	if strings.TrimSpace(prURL) == "" {
		return nil, fmt.Errorf("github: pull request URL is required for checks")
	}
	if err := c.AuthStatus(ctx); err != nil {
		return nil, err
	}
	res, err := c.run(ctx, "pr", "checks", prURL, "--json", "name,state,conclusion,workflowRunId,detailsUrl")
	if err != nil {
		return nil, fmt.Errorf("github: run gh pr checks: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("github: gh pr checks failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	var checks []Check
	if err := json.Unmarshal(res.Stdout, &checks); err != nil {
		return nil, fmt.Errorf("github: parse gh pr checks output: %w", err)
	}
	return checks, nil
}

func validateWorkflowRunID(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("github: workflow run ID is required")
	}
	if _, err := strconv.ParseInt(trimmed, 10, 64); err != nil {
		return fmt.Errorf("github: workflow run ID must be numeric, got %q", value)
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
