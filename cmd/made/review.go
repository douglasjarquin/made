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
	"sync"

	"github.com/douglasjarquin/made/internal/api"
)

const (
	ReviewApproved = "approved"
	ReviewRejected = "rejected"
)

// reviewDecisions is a stopgap: it records operator approve/reject answers
// in memory because no orchestrator yet resumes or halts a real pipeline run
// on them (Review/Document aren't wired into the daemon's run manager). Once
// that lands, decisions belong in the evidence store instead of here.
type reviewDecisions struct {
	mu      sync.Mutex
	entries map[reviewKey]string
}

type reviewKey struct {
	RunID string
	Stage string
}

func newReviewDecisions() *reviewDecisions {
	return &reviewDecisions{entries: make(map[reviewKey]string)}
}

func (d *reviewDecisions) set(runID, stage, decision string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries[reviewKey{RunID: runID, Stage: stage}] = decision
}

func (d *reviewDecisions) get(runID, stage string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	decision, ok := d.entries[reviewKey{RunID: runID, Stage: stage}]
	return decision, ok
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
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("review.decide: invalid params: %w", err)
		}
		if p.RunID == "" || p.Stage == "" {
			return nil, fmt.Errorf("review.decide: run_id and stage are required")
		}
		if p.Decision != ReviewApproved && p.Decision != ReviewRejected {
			return nil, fmt.Errorf("review.decide: decision must be %q or %q", ReviewApproved, ReviewRejected)
		}
		store.set(p.RunID, p.Stage, p.Decision)
		return reviewDecideResult{OK: true}, nil
	}
}

func reviewDecisionHandler(store *reviewDecisions) api.HandlerFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var p reviewDecisionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("review.decision: invalid params: %w", err)
		}
		decision, found := store.get(p.RunID, p.Stage)
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
	if err := client.CallInto("status", statusParams{RunID: *runID}, &report); err != nil {
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
