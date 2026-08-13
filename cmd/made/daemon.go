package main

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/douglasjarquin/made/internal/orchestrator"
)

const defaultIdleTimeout = 30 * time.Minute

const shutdownCancelTimeout = 5 * time.Second

func runDaemonCommand(args []string, stdout, stderr *os.File) int {
	if len(args) < 1 {
		_, _ = fmt.Fprintln(stderr, "usage: made daemon <start|stop|status>")
		return 2
	}

	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made daemon:", err)
		return 1
	}
	lockPath := filepath.Join(home, "daemon.lock")

	switch args[0] {
	case "start":
		return daemonStart(args[1:], home, lockPath, stdout, stderr)
	case "stop":
		return daemonStop(lockPath, stdout, stderr)
	case "status":
		return daemonStatus(lockPath, stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "made daemon: unknown subcommand %q\n", args[0])
		return 2
	}
}

func daemonStart(args []string, home, lockPath string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made daemon start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	idle := fs.Duration("idle-timeout", defaultIdleTimeout, "auto-stop after this much time with no active work")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ready := make(chan int, 1)
	_, done := startDaemon(context.Background(), home, lockPath, *idle, func(pid int) {
		ready <- pid
	})

	select {
	case pid := <-ready:
		_, _ = fmt.Fprintf(stdout, "made daemon: started (pid %d)\n", pid)
	case err := <-done:
		if errors.Is(err, daemon.ErrAlreadyRunning) {
			_, _ = fmt.Fprintln(stderr, "made daemon: already running")
			return 1
		}
		_, _ = fmt.Fprintln(stderr, "made daemon:", err)
		return 1
	}

	if err := <-done; err != nil {
		_, _ = fmt.Fprintln(stderr, "made daemon:", err)
		return 1
	}
	return 0
}

// startDaemon wires the socket API (Task 6) and the run manager (Task 9)
// into the daemon's blocking lifecycle (Task 5), since neither was wired
// into internal/daemon.Run itself: Options has no such hook, and adding one
// there would make internal/daemon depend on api/run-manager wiring choices
// that are really a cmd/made concern. Kept here instead of in internal/daemon
// so that package stays a pure lifecycle primitive.
//
// The returned RunManager lets a caller (currently only this file's tests)
// submit runs against the same daemon instance the socket server is serving.
// The returned channel receives daemon.Run's final error exactly once, after
// the socket server has also been shut down.
func startDaemon(ctx context.Context, home, lockPath string, idle time.Duration, onReady func(pid int)) (*daemon.RunManager, <-chan error) {
	rm := daemon.NewRunManager()
	reviewStore := daemon.NewReviewDecisions()
	srv := api.NewServer(api.SocketPath(home))
	registerDaemonHandlers(srv, rm, reviewStore)

	done := make(chan error, 1)

	if err := srv.Listen(); err != nil {
		done <- fmt.Errorf("listen on socket: %w", err)
		return rm, done
	}

	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(serveCtx) }()

	go func() {
		runErr := daemon.Run(ctx, daemon.Options{
			LockPath:    lockPath,
			IdleTimeout: idle,
			OnReady:     onReady,
			ActivityCh:  rm.ActivitySignal(),
		})
		cancelInFlightRuns(rm, shutdownCancelTimeout)
		cancelServe()
		<-serveErr
		_ = srv.Close()
		done <- runErr
	}()

	return rm, done
}

// cancelInFlightRuns runs between daemon.Run returning (SIGTERM, ctx
// cancellation, or idle timeout) and the socket server closing, so that
// `made daemon stop` never leaves an orphaned pipeline goroutine behind: a
// run's WorkFunc is only cooperative with cancellation, not instantly
// killable, so this blocks (up to timeout) for it to actually observe
// ctx.Done() and return before shutdown proceeds.
func cancelInFlightRuns(rm *daemon.RunManager, timeout time.Duration) {
	for _, snap := range rm.List() {
		if !isTerminalRunStatus(snap.Status) {
			_ = rm.Cancel(snap.ID)
		}
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		allTerminal := true
		for _, snap := range rm.List() {
			if !isTerminalRunStatus(snap.Status) {
				allTerminal = false
				break
			}
		}
		if allTerminal {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func isTerminalRunStatus(s daemon.RunStatus) bool {
	return s == daemon.RunCompleted || s == daemon.RunFailed
}

const debugHandlersEnv = "MADE_DEBUG_HANDLERS"

func registerDaemonHandlers(srv *api.Server, rm *daemon.RunManager, store *daemon.ReviewDecisions) {
	srv.Handle("status", statusHandler(rm))
	srv.Handle("review.decide", reviewDecideHandler(store))
	srv.Handle("review.decision", reviewDecisionHandler(store))
	srv.Handle("gate.admitPush", gateAdmitPushHandler())
	srv.Handle("gate.notifyPush", gateNotifyPushHandler(rm))
	if os.Getenv(debugHandlersEnv) == "1" {
		srv.Handle("debug.submitCancellableRun", debugSubmitCancellableRunHandler(rm))
	}
}

const gateAdmitPushCheckTimeout = 5 * time.Second

type gateAdmitPushParams struct {
	GatePath string `json:"gate_path"`
}

type gateAdmitPushResult struct {
	OK bool `json:"ok"`
}

// gateAdmitPushHandler is the fast pre-check a pre-receive hook shells out to
// before accepting a push: under the socket's owner-only trust model there is
// no caller identity to check, so admission degrades to "is this a gate the
// daemon recognizes" - a real, valid bare repo on disk. It deliberately does
// not touch RunManager; creating a run is the orchestrator's job, not
// admission's.
func gateAdmitPushHandler() api.HandlerFunc {
	return func(_ context.Context, params json.RawMessage) (any, error) {
		var p gateAdmitPushParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("gate.admitPush: invalid params: %w", err)
		}
		if p.GatePath == "" {
			return nil, fmt.Errorf("gate.admitPush: gate_path is required")
		}
		if err := validateBareGateRepo(p.GatePath); err != nil {
			return nil, fmt.Errorf("gate.admitPush: %w", err)
		}
		return gateAdmitPushResult{OK: true}, nil
	}
}

func validateBareGateRepo(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("gate path %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("gate path %s is not a directory", path)
	}

	ctx, cancel := context.WithTimeout(context.Background(), gateAdmitPushCheckTimeout)
	defer cancel()

	res, err := exec.Run(ctx, exec.Command{
		Name: "git",
		Args: []string{"-C", path, "rev-parse", "--is-bare-repository"},
	})
	if err != nil {
		return fmt.Errorf("check bare repository at %s: %w", path, err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s is not a git repository: %s", path, strings.TrimSpace(string(res.Stderr)))
	}
	if got := strings.TrimSpace(string(res.Stdout)); got != "true" {
		return fmt.Errorf("%s is not a bare repository (git rev-parse --is-bare-repository reported %q)", path, got)
	}
	return nil
}

const gateNotifyPushDefaultBranchTimeout = 10 * time.Second

type gateNotifyPushParams struct {
	GatePath string `json:"gate_path"`
	OldSHA   string `json:"old_sha"`
	NewSHA   string `json:"new_sha"`
	Ref      string `json:"ref"`
}

type gateNotifyPushResult struct {
	RunID string `json:"run_id,omitempty"`
}

// gateNotifyPushHandler is the post-receive-driven counterpart to
// gate.admitPush: where admission is a fast yes/no pre-check, this is where
// a run actually gets created. gitgate.ClassifyRef (Task 8) decides whether
// the ref is run-eligible at all; SupersedeQueued (this task) then drops any
// still-queued run for the same branch before submitting this push's own
// run, so a rapid second push always wins over a first one that hasn't
// started yet - never over one already running.
func gateNotifyPushHandler(rm *daemon.RunManager) api.HandlerFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var p gateNotifyPushParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("gate.notifyPush: invalid params: %w", err)
		}
		if p.GatePath == "" || p.Ref == "" || p.NewSHA == "" {
			return nil, fmt.Errorf("gate.notifyPush: gate_path, ref, and new_sha are required")
		}

		branchCtx, cancel := context.WithTimeout(ctx, gateNotifyPushDefaultBranchTimeout)
		defer cancel()
		defaultBranch, err := resolveDefaultBranch(branchCtx, p.GatePath)
		if err != nil {
			return nil, fmt.Errorf("gate.notifyPush: resolve default branch: %w", err)
		}

		decision := gitgate.ClassifyRef(p.Ref, defaultBranch, p.OldSHA, p.NewSHA)
		if !decision.Accept {
			return nil, fmt.Errorf("gate.notifyPush: %s", decision.Message)
		}
		if !decision.CreateRun {
			return gateNotifyPushResult{}, nil
		}

		branch := strings.TrimPrefix(p.Ref, "refs/heads/")
		repo := gateRepoIdentifier(p.GatePath)
		rm.SupersedeQueued(repo, branch)

		gatePath := p.GatePath
		worktreesDir := gitgate.WorktreesDir(gatePath)
		newSHA := p.NewSHA
		runID := rm.NewRunID()

		work := func(workCtx context.Context, emit func(daemon.Event)) error {
			return orchestrator.Run(workCtx, gatePath, defaultBranch, worktreesDir, runID, newSHA, func(_ context.Context, rc *orchestrator.RunContext) error {
				emit(daemon.Event{Kind: daemon.EventStageStarted, Stage: "setup", Message: fmt.Sprintf("checked out %s", newSHA)})
				emit(daemon.Event{Kind: daemon.EventStageFinished, Stage: "setup"})
				_ = rc
				return nil
			})
		}

		if _, err := rm.Submit(runID, repo, branch, work); err != nil {
			return nil, fmt.Errorf("gate.notifyPush: submit run: %w", err)
		}

		return gateNotifyPushResult{RunID: runID}, nil
	}
}

type debugSubmitCancellableRunParams struct {
	ID             string `json:"id"`
	Repo           string `json:"repo"`
	Branch         string `json:"branch"`
	SideEffectFile string `json:"side_effect_file"`
}

// debugSubmitCancellableRunHandler exists only for this package's real
// subprocess-level daemon-stop test (there is no production run-submission
// endpoint yet - that lands with the orchestrator in a later task), gated
// behind MADE_DEBUG_HANDLERS so it is never registered outside a test.
func debugSubmitCancellableRunHandler(rm *daemon.RunManager) api.HandlerFunc {
	return func(_ context.Context, params json.RawMessage) (any, error) {
		var p debugSubmitCancellableRunParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("decode params: %w", err)
		}
		work := func(workCtx context.Context, _ func(daemon.Event)) error {
			<-workCtx.Done()
			if p.SideEffectFile != "" {
				_ = os.WriteFile(p.SideEffectFile, []byte("cancelled"), 0o600)
			}
			return workCtx.Err()
		}
		return rm.Submit(p.ID, p.Repo, p.Branch, work)
	}
}

func daemonStop(lockPath string, stdout, stderr *os.File) int {
	if err := daemon.Stop(lockPath, 10*time.Second); err != nil {
		_, _ = fmt.Fprintln(stderr, "made daemon:", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "made daemon: stopped")
	return 0
}

func daemonStatus(lockPath string, stdout, stderr *os.File) int {
	st, err := daemon.Status(lockPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made daemon:", err)
		return 1
	}
	if st.Running {
		_, _ = fmt.Fprintf(stdout, "made daemon: running (pid %d)\n", st.PID)
		return 0
	}
	_, _ = fmt.Fprintln(stdout, "made daemon: not running")
	return 0
}

func madeHome() (string, error) {
	dir := os.Getenv("MADE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		dir = filepath.Join(home, ".made")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create made home %s: %w", dir, err)
	}
	return dir, nil
}
