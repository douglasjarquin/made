package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
)

const (
	ReviewApproved = daemon.ReviewApproved
	ReviewRejected = daemon.ReviewRejected
)

// reviewDecisions now lives in internal/daemon (co-located with RunManager,
// since a decision is per-run state) so Task 12's orchestrator can reach the
// same store these RPC handlers use; this alias keeps the RPC-facing code
// below unchanged.
type reviewDecisions = daemon.ReviewDecisions

func newReviewDecisions() *reviewDecisions {
	return daemon.NewReviewDecisions()
}

type reviewDecideParams struct {
	RunID    string `json:"run_id"`
	Stage    string `json:"stage"`
	Decision string `json:"decision"`
}

type reviewDecideResult struct {
	OK bool `json:"ok"`
}

type reviewDecisionParams struct {
	RunID string `json:"run_id"`
	Stage string `json:"stage"`
}

type reviewDecisionResult struct {
	Decision string `json:"decision"`
	Found    bool   `json:"found"`
}

func reviewDecideHandler(store *reviewDecisions) api.HandlerFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var p reviewDecideParams
		if err := decodeStrictJSON(params, &p); err != nil {
			return nil, fmt.Errorf("review.decide: invalid params: %w", err)
		}
		if p.RunID == "" || p.Stage == "" {
			return nil, fmt.Errorf("review.decide: run_id and stage are required")
		}
		if p.Decision != ReviewApproved && p.Decision != ReviewRejected {
			return nil, fmt.Errorf("review.decide: decision must be %q or %q", ReviewApproved, ReviewRejected)
		}
		if err := store.Set(p.RunID, p.Stage, p.Decision); err != nil {
			return nil, err
		}
		return reviewDecideResult{OK: true}, nil
	}
}

func reviewDecisionHandler(store *reviewDecisions) api.HandlerFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var p reviewDecisionParams
		if err := decodeStrictJSON(params, &p); err != nil {
			return nil, fmt.Errorf("review.decision: invalid params: %w", err)
		}
		decision, found := store.Get(p.RunID, p.Stage)
		return reviewDecisionResult{Decision: decision, Found: found}, nil
	}
}

func runReviewCommand(args []string, stdin io.Reader, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made review", flag.ContinueOnError)
	fs.SetOutput(stderr)
	runID := fs.String("run", "", "run ID to review (default: most recent run)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *runID == "" {
		_, _ = fmt.Fprintln(stderr, "made review: --run exact-run-id is required")
		return 2
	}

	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made review:", err)
		return 1
	}

	client, err := api.Dial(api.SocketPath(home))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made review: daemon not reachable:", err)
		return 1
	}
	defer func() { _ = client.Close() }()

	var report StatusReport
	if err := client.CallInto("run.status", statusParams{RunID: *runID}, &report); err != nil {
		_, _ = fmt.Fprintln(stderr, "made review:", err)
		return 1
	}

	if len(report.PendingFindings) == 0 {
		_, _ = fmt.Fprintln(stdout, "made review: no pending findings")
		return 0
	}

	scanner := bufio.NewScanner(stdin)
	anyRejected := false
	for _, f := range report.PendingFindings {
		_, _ = fmt.Fprintf(stdout, "[%s] %s\n", f.Stage, f.Message)
		_, _ = fmt.Fprint(stdout, "approve/reject? [a/r]: ")

		decision, err := readDecision(scanner)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "made review:", err)
			return 1
		}

		if err := client.CallInto("review.decide", reviewDecideParams{
			RunID:    report.RunID,
			Stage:    f.Stage,
			Decision: decision,
		}, nil); err != nil {
			_, _ = fmt.Fprintln(stderr, "made review:", err)
			return 1
		}

		_, _ = fmt.Fprintf(stdout, "%s: %s\n", decision, f.Stage)
		if decision == ReviewRejected {
			anyRejected = true
		}
	}

	if anyRejected {
		_, _ = fmt.Fprintln(stdout, "made review: one or more findings rejected; pipeline halted")
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "made review: all findings approved; pipeline resumed")
	return 0
}

func readDecision(scanner *bufio.Scanner) (string, error) {
	for scanner.Scan() {
		switch strings.TrimSpace(strings.ToLower(scanner.Text())) {
		case "a", "approve":
			return ReviewApproved, nil
		case "r", "reject":
			return ReviewRejected, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read decision: %w", err)
	}
	return "", fmt.Errorf("no approve/reject decision provided")
}
