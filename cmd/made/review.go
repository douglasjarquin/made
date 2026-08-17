package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
)

const (
	ReviewApproved = daemon.ReviewApproved
	ReviewRejected = daemon.ReviewRejected
)

type reviewDecideParams struct {
	RunID    string `json:"run_id"`
	Stage    string `json:"stage"`
	Decision string `json:"decision"`
}

type reviewDecisionReport struct {
	SchemaVersion   int    `json:"schema_version"`
	ProtocolVersion int    `json:"protocol_version"`
	RunID           string `json:"run_id"`
	Stage           string `json:"stage"`
	Decision        string `json:"decision"`
}

func reviewDecideRunHandler(rm *daemon.RunManager, store *daemon.ReviewDecisions) api.HandlerFunc {
	return func(_ context.Context, params json.RawMessage) (any, error) {
		var p reviewDecideParams
		if err := decodeStrictParams(params, &p); err != nil {
			return nil, fmt.Errorf("review.decide: invalid params: %w", err)
		}
		if p.RunID == "" || p.Stage == "" {
			return nil, fmt.Errorf("review.decide: run_id and stage are required")
		}
		if p.Decision != ReviewApproved && p.Decision != ReviewRejected {
			return nil, fmt.Errorf("review.decide: decision must be %q or %q", ReviewApproved, ReviewRejected)
		}
		if _, ok := rm.Snapshot(p.RunID); !ok {
			return nil, fmt.Errorf("review.decide: exact run_id %q was not found", p.RunID)
		}
		if err := rm.SetDecision(p.RunID, p.Stage, p.Decision); err != nil {
			return nil, err
		}
		store.Set(p.RunID, p.Stage, p.Decision)
		return reviewDecisionReport{
			SchemaVersion: 1, ProtocolVersion: api.Version,
			RunID: p.RunID, Stage: p.Stage, Decision: p.Decision,
		}, nil
	}
}

func runReviewDecideCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made review decide", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "output JSON")
	stage := fs.String("stage", "", "stage name")
	decision := fs.String("decision", "", "approved or rejected")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*jsonOutput || fs.NArg() != 1 || *stage == "" || (*decision != ReviewApproved && *decision != ReviewRejected) {
		_, _ = fmt.Fprintln(stderr, "usage: made review decide --json --stage <stage> --decision <approved|rejected> <run-id>")
		return 2
	}
	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made review decide:", err)
		return 1
	}
	client, err := api.Dial(api.SocketPath(home))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made review decide: daemon not reachable:", err)
		return 1
	}
	defer func() { _ = client.Close() }()
	var report reviewDecisionReport
	if err := client.CallInto("review.decide", reviewDecideParams{RunID: fs.Arg(0), Stage: *stage, Decision: *decision}, &report); err != nil {
		_, _ = fmt.Fprintln(stderr, "made review decide:", err)
		return 1
	}
	return writeJSON(stdout, report, stderr, "made review decide")
}
