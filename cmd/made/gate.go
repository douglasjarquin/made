package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
	"github.com/douglasjarquin/made/internal/exec"
	"github.com/douglasjarquin/made/internal/gitgate"
)

const gateCommandTimeout = 30 * time.Second

const gitZeroSHAValue = "0000000000000000000000000000000000000000"

func runGateCommand(args []string, stdout, stderr *os.File) int {
	if len(args) < 1 {
		_, _ = fmt.Fprintln(stderr, "usage: made gate init <target-repo-path> <real-remote-url>")
		return 2
	}

	switch args[0] {
	case "init":
		return runGateInitCommand(args[1:], stdout, stderr)
	case "admit-push":
		return runGateAdmitPushCommand(args[1:], stdout, stderr)
	case "notify-push":
		return runGateNotifyPushCommand(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "made gate: unknown subcommand %q\n", args[0])
		return 2
	}
}

// gateRepoIdentifier recovers the repo-hash identifier gitgate.GatePath
// embeds in a gate's own directory layout (<madeHome>/gates/<hash>/gate.git)
// directly from a GatePath value, so RunManager's per-repo serialization key
// matches the exact same target-repo identity Task 6 already established -
// without needing madeHome or the original target-repo path at
// notify-push time, when only the bare gate's own path is on hand.
func gateRepoIdentifier(gatePath string) string {
	return filepath.Base(filepath.Dir(gatePath))
}

// runGateNotifyPushCommand backs the post-receive hook, which must never
// fail the git push it is reacting to: every error path here is logged to
// stderr for debuggability and still returns exit code 0.
func runGateNotifyPushCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made gate notify-push", flag.ContinueOnError)
	fs.SetOutput(stderr)
	gatePath := fs.String("gate", "", "path to the bare gate repository")
	oldSHA := fs.String("old", "", "old SHA from the ref update")
	newSHA := fs.String("new", "", "new SHA from the ref update")
	ref := fs.String("ref", "", "the ref that was updated")
	if err := fs.Parse(args); err != nil {
		_, _ = fmt.Fprintln(stderr, "gate notify-push: parse args:", err)
		return 0
	}

	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gate notify-push:", err)
		return 0
	}

	client, err := api.Dial(api.SocketPath(home))
	if err != nil {
		queueErr := queueOfflineGateSubmission(home, *gatePath, *ref, *newSHA)
		if queueErr != nil {
			_, _ = fmt.Fprintln(stderr, "gate notify-push: dial daemon:", err, "; durable queue:", queueErr)
		} else if *newSHA == gitZeroSHAValue {
			_, _ = fmt.Fprintln(stderr, "gate notify-push: ref deletion does not require a run:", err)
		} else {
			_, _ = fmt.Fprintln(stderr, "gate notify-push: daemon unavailable; submission durably queued:", err)
		}
		return 0
	}
	defer func() { _ = client.Close() }()

	var result gateNotifyPushResult
	if err := client.CallInto("gate.notifyPush", gateNotifyPushParams{
		GatePath: *gatePath,
		OldSHA:   *oldSHA,
		NewSHA:   *newSHA,
		Ref:      *ref,
	}, &result); err != nil {
		if *newSHA == gitZeroSHAValue {
			_, _ = fmt.Fprintln(stderr, "gate notify-push:", err, "; ref deletion does not require a run")
			return 0
		}
		queueErr := queueOfflineGateSubmission(home, *gatePath, *ref, *newSHA)
		if queueErr != nil {
			_, _ = fmt.Fprintln(stderr, "gate notify-push:", err, "; durable queue:", queueErr)
		} else {
			_, _ = fmt.Fprintln(stderr, "gate notify-push:", err, "; submission durably queued")
		}
		return 0
	}

	if result.RunID == "" {
		_, _ = fmt.Fprintln(stdout, "gate notify-push: ok (no run)")
		return 0
	}
	_, _ = fmt.Fprintf(stdout, "gate notify-push: ok (run %s)\n", result.RunID)
	return 0
}

func queueOfflineGateSubmission(home, gatePath, ref, newSHA string) error {
	if newSHA == gitZeroSHAValue {
		return nil
	}
	spool, err := daemon.OpenGateSpool(filepath.Join(home, "gate.spool"))
	if err != nil {
		return err
	}
	_, _, err = spool.Enqueue(daemon.GateSubmission{Gate: gatePath, Ref: ref, SHA: newSHA, RunID: daemon.NewRunID()})
	return err
}

func runGateAdmitPushCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made gate admit-push", flag.ContinueOnError)
	fs.SetOutput(stderr)
	gatePath := fs.String("gate", "", "path to the bare gate repository")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *gatePath == "" {
		_, _ = fmt.Fprintln(stderr, "usage: made gate admit-push --gate <path>")
		return 2
	}

	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made gate admit-push:", err)
		return 1
	}

	client, err := api.Dial(api.SocketPath(home))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made gate admit-push:", err)
		return 1
	}
	defer func() { _ = client.Close() }()

	if _, err := client.Call("gate.admitPush", gateAdmitPushParams{GatePath: *gatePath}); err != nil {
		_, _ = fmt.Fprintln(stderr, "made gate admit-push:", err)
		return 1
	}

	_, _ = fmt.Fprintln(stdout, "gate admit-push: ok")
	return 0
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
