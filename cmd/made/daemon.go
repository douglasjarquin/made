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
	"sync"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
	"github.com/douglasjarquin/made/internal/exec"
	"github.com/douglasjarquin/made/internal/gitgate"
	"github.com/douglasjarquin/made/internal/orchestrator"
	"golang.org/x/sys/unix"
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
		return daemonStop(api.SocketPath(home), stdout, stderr)
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
	validatedHome, err := ensureMadeHome(home)
	if err != nil {
		done := make(chan error, 1)
		done <- err
		return daemon.NewRunManager(), done
	}
	home = validatedHome
	ownedLock, err := daemon.AcquireLock(lockPath)
	if err != nil {
		done := make(chan error, 1)
		done <- err
		return daemon.NewRunManager(), done
	}
	spool, err := daemon.OpenGateSpool(filepath.Join(home, "gate.spool"))
	if err != nil {
		_ = ownedLock.Release()
		done := make(chan error, 1)
		done <- err
		return daemon.NewRunManager(), done
	}
	rm, err := daemon.NewPersistentRunManager(filepath.Join(home, "runs.wal"))
	if err != nil {
		_ = ownedLock.Release()
		done := make(chan error, 1)
		done <- err
		return daemon.NewRunManager(), done
	}
	socketPath := api.SocketPath(home)
	if err := api.PrepareSocket(socketPath); err != nil {
		_ = ownedLock.Release()
		done := make(chan error, 1)
		done <- err
		return rm, done
	}
	reviewStore := daemon.NewReviewDecisions()
	admission := &sync.Mutex{}
	runCtx, cancelRun := context.WithCancel(ctx)
	srv := api.NewServer(socketPath)
	registerDaemonHandlers(srv, rm, reviewStore, spool, cancelRun, admission)

	done := make(chan error, 1)

	if err := srv.Listen(); err != nil {
		_ = ownedLock.Release()
		done <- fmt.Errorf("listen on socket: %w", err)
		return rm, done
	}

	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(serveCtx) }()

	go func() {
		runErr := daemon.Run(runCtx, daemon.Options{
			LockPath:      lockPath,
			Lock:          ownedLock,
			IdleTimeout:   idle,
			OnReady:       onReady,
			ActivityCh:    rm.ActivitySignal(),
			ActiveFunc:    rm.HasActive,
			UndrainedFunc: spool.HasPending,
		})
		admission.Lock()
		rm.StopAccepting()
		admission.Unlock()
		if cancelErr := cancelInFlightRuns(rm, shutdownCancelTimeout); cancelErr != nil {
			runErr = errors.Join(runErr, cancelErr)
		}
		cancelRun()
		cancelServe()
		<-serveErr
		_ = srv.Close()
		done <- runErr
	}()

	go replayPendingSubmissions(runCtx, rm, reviewStore, spool, admission)

	return rm, done
}

func replayPendingSubmissions(ctx context.Context, rm *daemon.RunManager, reviewDecisions *daemon.ReviewDecisions, spool *daemon.GateSpool, admission ...*sync.Mutex) {
	handler := gateNotifyPushHandler(rm, reviewDecisions, spool, admission...)
	for {
		pending := spool.Pending()
		if len(pending) == 0 {
			return
		}
		for _, submission := range pending {
			params, err := json.Marshal(gateNotifyPushParams{
				GatePath: submission.Gate,
				Ref:      submission.Ref,
				NewSHA:   submission.SHA,
				RunID:    submission.RunID,
				Replay:   true,
			})
			if err != nil {
				return
			}
			if _, err := handler(ctx, params); err != nil {
				continue
			}
		}
		if !spool.HasPending() {
			return
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
	}
}

// cancelInFlightRuns runs between daemon.Run returning (SIGTERM, ctx
// cancellation, or idle timeout) and the socket server closing, so that
// `made daemon stop` never leaves an orphaned pipeline goroutine behind: a
// run's WorkFunc is only cooperative with cancellation, not instantly
// killable, so this blocks (up to timeout) for it to actually observe
// ctx.Done() and return before shutdown proceeds.

func cancelInFlightRuns(rm *daemon.RunManager, timeout time.Duration) error {
	var firstErr error
	for _, snap := range rm.List() {
		if !isTerminalRunStatus(snap.Status) {
			if err := rm.Cancel(snap.ID); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("cancel run %q during shutdown: %w", snap.ID, err)
			}
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
			return firstErr
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, snap := range rm.List() {
		if !isTerminalRunStatus(snap.Status) {
			return errors.Join(firstErr, fmt.Errorf("shutdown: timed out waiting for run %q to finish", snap.ID))
		}
	}
	return firstErr
}

func isTerminalRunStatus(s daemon.RunStatus) bool {
	return s == daemon.RunSucceeded || s == daemon.RunFailed || s == daemon.RunCanceled || s == daemon.RunSuperseded
}

const debugHandlersEnv = "MADE_DEBUG_HANDLERS"

func registerDaemonHandlers(srv *api.Server, rm *daemon.RunManager, store *daemon.ReviewDecisions, spool *daemon.GateSpool, cancel context.CancelFunc, admission ...*sync.Mutex) {
	srv.Handle("run.status", runStatusHandler(rm))
	srv.Handle("run.submit", runSubmitHandler(rm, admission...))
	srv.Handle("run.list", runListHandler(rm))
	srv.Handle("run.cancel", runCancelHandler(rm))
	srv.Handle("review.decide", reviewDecideRunHandler(rm, store))
	srv.Handle("daemon.shutdown", daemonShutdownHandler(rm, spool, cancel, admission...))
	srv.Handle("gate.admitPush", gateAdmitPushHandler())
	srv.Handle("gate.notifyPush", gateNotifyPushHandler(rm, store, spool, admission...))
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
	RunID    string `json:"run_id,omitempty"`
	Replay   bool   `json:"replay,omitempty"`
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
func gateNotifyPushHandler(rm *daemon.RunManager, reviewDecisions *daemon.ReviewDecisions, spool *daemon.GateSpool, admission ...*sync.Mutex) api.HandlerFunc {
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
		refspec := fmt.Sprintf("%s:refs/heads/%s", defaultBranch, defaultBranch)
		if err := runGit(branchCtx, p.GatePath, "fetch", "origin", refspec); err != nil {
			return nil, fmt.Errorf("gate.notifyPush: refresh default branch %s: %w", defaultBranch, err)
		}

		decision := gitgate.ClassifyRef(p.Ref, defaultBranch, p.OldSHA, p.NewSHA)
		if !decision.Accept {
			if p.Replay {
				if err := spool.Drain(daemon.GateSubmission{Gate: p.GatePath, Ref: p.Ref, SHA: p.NewSHA, RunID: p.RunID}); err != nil {
					return nil, fmt.Errorf("gate.notifyPush: drain rejected replay: %w", err)
				}
				return gateNotifyPushResult{}, nil
			}
			return nil, fmt.Errorf("gate.notifyPush: %s", decision.Message)
		}
		if !decision.CreateRun {
			return gateNotifyPushResult{}, nil
		}

		unlock := lockAdmission(admission)
		defer unlock()
		if !rm.Accepting() {
			return nil, daemon.ErrRunSubmissionClosed
		}

		branch := strings.TrimPrefix(p.Ref, "refs/heads/")
		repo := gateRepoIdentifier(p.GatePath)

		gatePath := p.GatePath
		worktreesDir := gitgate.WorktreesDir(gatePath)
		newSHA := p.NewSHA
		runID := p.RunID
		if runID == "" {
			runID = rm.NewRunID()
		}
		submission, created, err := spool.Enqueue(daemon.GateSubmission{Gate: p.GatePath, Ref: p.Ref, SHA: p.NewSHA, RunID: runID})
		if err != nil {
			return nil, fmt.Errorf("gate.notifyPush: enqueue submission: %w", err)
		}
		if !created {
			if _, ok := rm.Snapshot(submission.RunID); ok {
				if err := rm.AppendSubmissionEvent(submission.RunID, daemon.SubmissionEvent{Gate: p.GatePath, Ref: p.Ref, InputSHA: p.NewSHA, Kind: "push"}); err != nil {
					return nil, fmt.Errorf("gate.notifyPush: persist replayed submission event: %w", err)
				}
				if err := spool.Drain(submission); err != nil {
					return nil, fmt.Errorf("gate.notifyPush: drain replayed submission: %w", err)
				}
				return gateNotifyPushResult{RunID: submission.RunID}, nil
			}
			runID = submission.RunID
		}
		if err := rm.SupersedeQueued(repo, branch); err != nil {
			return nil, fmt.Errorf("gate.notifyPush: supersede queued runs: %w", err)
		}

		work := func(workCtx context.Context, emit func(daemon.Event)) error {
			return orchestrator.Run(workCtx, gatePath, defaultBranch, worktreesDir, runID, newSHA,
				orchestrator.NewWorkFunc(rm, reviewDecisions, emit, runID, defaultBranch, branch, orchestrator.Options{}))
		}

		if _, err := rm.SubmitWithMetadata(runID, repo, branch, p.NewSHA, "", work); err != nil {
			return nil, fmt.Errorf("gate.notifyPush: submit run: %w", err)
		}
		if err := rm.AppendSubmissionEvent(runID, daemon.SubmissionEvent{Gate: p.GatePath, Ref: p.Ref, InputSHA: p.NewSHA, Kind: "push"}); err != nil {
			return nil, fmt.Errorf("gate.notifyPush: persist submission event: %w", err)
		}
		if err := spool.Drain(submission); err != nil {
			return nil, fmt.Errorf("gate.notifyPush: drain submission: %w", err)
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

func daemonStop(socketPath string, stdout, stderr *os.File) int {
	client, err := api.Dial(socketPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made daemon: dial shutdown socket:", err)
		return 1
	}
	defer func() { _ = client.Close() }()
	if _, err := client.Call("daemon.shutdown", nil); err != nil {
		_, _ = fmt.Fprintln(stderr, "made daemon:", err)
		return 1
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		probe, probeErr := api.Dial(socketPath)
		if probeErr != nil {
			_, _ = fmt.Fprintln(stdout, "made daemon: stopped")
			return 0
		}
		_ = probe.Close()
		time.Sleep(20 * time.Millisecond)
	}
	_, _ = fmt.Fprintln(stderr, "made daemon: shutdown timed out")
	return 1
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
	return ensureMadeHome(dir)
}

func ensureMadeHome(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve made home %s: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return "", fmt.Errorf("create made home %s: %w", abs, err)
	}
	fd, err := unix.Open(abs, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", fmt.Errorf("open made home %s: %w", abs, err)
	}
	defer func() { _ = unix.Close(fd) }()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return "", fmt.Errorf("inspect made home %s: %w", abs, err)
	}
	if uint32(stat.Uid) != uint32(os.Getuid()) {
		return "", fmt.Errorf("made home %s is not owned by the current user", abs)
	}
	if stat.Mode&0o077 != 0 {
		if err := unix.Fchmod(fd, 0o700); err != nil {
			return "", fmt.Errorf("restrict made home %s: %w", abs, err)
		}
	}
	return abs, nil
}
