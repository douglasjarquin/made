package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/douglasjarquin/made/internal/exec"
)

type Client struct {
	Dir      string
	Binary   string
	ExtraEnv []string
	Timeout  time.Duration

	authMu    sync.Mutex
	authUntil time.Time
}

const authCacheTTL = time.Minute

type AuthError struct {
	Detail string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("github: not authenticated: %s", e.Detail)
}

type RateLimitError struct {
	Operation string
	Detail    string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github: rate limit during %s: %s", e.Operation, e.Detail)
}

type CreatePROptions struct {
	Title string
	Body  string
	Base  string
	Head  string
}

func (c *Client) AuthStatus(ctx context.Context) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if time.Now().Before(c.authUntil) {
		return nil
	}

	res, err := c.run(ctx, "auth", "status")
	if err != nil {
		return fmt.Errorf("github: run gh auth status: %w", err)
	}
	if res.ExitCode != 0 {
		detail := strings.TrimSpace(string(res.Stderr))
		if isRateLimitDetail(detail) {
			return &RateLimitError{Operation: "gh auth status", Detail: detail}
		}
		return &AuthError{Detail: detail}
	}
	c.authUntil = time.Now().Add(authCacheTTL)
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
		return "", fmt.Errorf("github: parse gh pr view output: %w", err)
	}
	return payload.MergeStateStatus, nil
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

func commandFailure(operation string, res *exec.Result) error {
	detail := strings.TrimSpace(string(res.Stderr))
	if isRateLimitDetail(detail) {
		return &RateLimitError{Operation: operation, Detail: detail}
	}
	return fmt.Errorf("github: %s failed: %s", operation, detail)
}

func isRateLimitDetail(detail string) bool {
	lower := strings.ToLower(detail)
	return strings.Contains(lower, "rate limit") || strings.Contains(lower, "api rate limit exceeded")
}
