package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/exec"
	"github.com/douglasjarquin/made/internal/gitgate"
)

const gateCommandTimeout = 30 * time.Second

func runGateCommand(args []string, stdout, stderr *os.File) int {
	if len(args) < 1 {
		_, _ = fmt.Fprintln(stderr, "usage: made gate init <target-repo-path> <real-remote-url>")
		return 2
	}

	switch args[0] {
	case "init":
		return runGateInitCommand(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "made gate: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runGateInitCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made gate init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		_, _ = fmt.Fprintln(stderr, "usage: made gate init <target-repo-path> <real-remote-url>")
		return 2
	}
	targetRepoPath := fs.Arg(0)
	remoteURL := fs.Arg(1)

	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made gate init:", err)
		return 1
	}

	madeBinaryPath, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made gate init: resolve made binary path:", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), gateCommandTimeout)
	defer cancel()

	barePath, err := gateInit(ctx, home, madeBinaryPath, targetRepoPath, remoteURL)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made gate init:", err)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "gate initialized at %s\n", barePath)
	return 0
}

// gateInit is idempotent by design: running it twice against the same
// target repo heals/updates existing state (remotes are pointed at the
// given URL rather than re-added, the default branch is re-fetched to its
// current tip, hooks are rewritten) instead of refusing. This matches how
// consigliere's onboarding flow is expected to call `made gate init`
// defensively without first checking whether a gate already exists.
func gateInit(ctx context.Context, madeHomeDir, madeBinaryPath, targetRepoPath, remoteURL string) (string, error) {
	absTarget, err := filepath.Abs(targetRepoPath)
	if err != nil {
		return "", fmt.Errorf("resolve target repo path %s: %w", targetRepoPath, err)
	}
	if resolved, err := filepath.EvalSymlinks(absTarget); err == nil {
		absTarget = resolved
	}

	barePath := gitgate.GatePath(madeHomeDir, absTarget)

	if err := gitgate.InitBare(barePath); err != nil {
		return "", err
	}

	if err := ensureRemote(ctx, barePath, "origin", remoteURL); err != nil {
		return "", err
	}

	if err := runGit(ctx, barePath, "config", "made.real-remote", remoteURL); err != nil {
		return "", err
	}

	// git remote show origin's "HEAD branch:" line is the simplest reliable
	// way to learn the real remote's default branch: it reads the remote's
	// own advertised HEAD symref rather than guessing from the local
	// checkout or a --default-branch flag the caller would have to know in
	// advance.
	defaultBranch, err := resolveDefaultBranch(ctx, barePath)
	if err != nil {
		return "", err
	}

	refspec := fmt.Sprintf("%s:refs/heads/%s", defaultBranch, defaultBranch)
	if err := runGit(ctx, barePath, "fetch", "origin", refspec); err != nil {
		return "", fmt.Errorf("fetch default branch %s: %w", defaultBranch, err)
	}

	if err := gitgate.InstallHooks(barePath, madeBinaryPath, madeHomeDir); err != nil {
		return "", err
	}

	if err := ensureRemote(ctx, absTarget, "made", barePath); err != nil {
		return "", err
	}

	return barePath, nil
}

func ensureRemote(ctx context.Context, repoDir, name, url string) error {
	res, err := exec.Run(ctx, exec.Command{Name: "git", Args: []string{"remote", "get-url", name}, Dir: repoDir})
	if err != nil {
		return fmt.Errorf("git remote get-url %s: %w", name, err)
	}
	if res.ExitCode == 0 {
		return runGit(ctx, repoDir, "remote", "set-url", name, url)
	}
	return runGit(ctx, repoDir, "remote", "add", name, url)
}

func resolveDefaultBranch(ctx context.Context, barePath string) (string, error) {
	res, err := exec.Run(ctx, exec.Command{Name: "git", Args: []string{"remote", "show", "origin"}, Dir: barePath})
	if err != nil {
		return "", fmt.Errorf("git remote show origin: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("git remote show origin failed: %s", strings.TrimSpace(string(res.Stderr)+string(res.Stdout)))
	}

	for _, line := range strings.Split(string(res.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "HEAD branch:") {
			continue
		}
		branch := strings.TrimSpace(strings.TrimPrefix(line, "HEAD branch:"))
		if branch == "" || branch == "(unknown)" {
			return "", fmt.Errorf("could not resolve default branch: origin's HEAD branch is %q", branch)
		}
		return branch, nil
	}
	return "", fmt.Errorf("could not find \"HEAD branch:\" in git remote show origin output")
}

func runGit(ctx context.Context, dir string, args ...string) error {
	res, err := exec.Run(ctx, exec.Command{Name: "git", Args: args, Dir: dir})
	if err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("git %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(res.Stderr)+string(res.Stdout)))
	}
	return nil
}
