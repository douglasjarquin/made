package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/douglasjarquin/made/internal/receipt"
)

const receiptsCommandTimeout = 30 * time.Second

func runReceiptsCommand(args []string, stdout, stderr *os.File) int {
	if len(args) < 1 || args[0] != "list" {
		_, _ = fmt.Fprintln(stderr, "usage: made receipts list [--json] [--repo <path>]")
		return 2
	}
	return runReceiptsListCommand(args[1:], stdout, stderr)
}

func runReceiptsListCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made receipts list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	repoPath := fs.String("repo", ".", "path to the repository whose receipts branch to read")
	branch := fs.String("branch", "", "receipts branch name (defaults to made-receipts)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made receipts list [--json] [--repo <path>]")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), receiptsCommandTimeout)
	defer cancel()

	store := &receipt.Store{RepoPath: *repoPath, Branch: *branch}
	receipts, err := store.List(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made receipts list:", err)
		return 1
	}

	if *asJSON {
		encoder := json.NewEncoder(stdout)
		if err := encoder.Encode(receiptsListReport{Receipts: receiptSummaries(receipts)}); err != nil {
			_, _ = fmt.Fprintln(stderr, "made receipts list:", err)
			return 1
		}
		return 0
	}

	if len(receipts) == 0 {
		_, _ = fmt.Fprintln(stdout, "No receipts found.")
		return 0
	}
	for _, r := range receipts {
		age := time.Since(r.CompletedAt).Round(time.Second)
		_, _ = fmt.Fprintf(stdout, "%s  lane=%-12s run=%-20s completed=%s ago\n", r.Fingerprint.Hash(), r.Fingerprint.Lane, r.SourceRunID, age)
	}
	return 0
}

type receiptsListReport struct {
	Receipts []receiptSummary `json:"receipts"`
}

type receiptSummary struct {
	Fingerprint string    `json:"fingerprint"`
	Lane        string    `json:"lane"`
	SourceRunID string    `json:"source_run_id"`
	CompletedAt time.Time `json:"completed_at"`
}

func receiptSummaries(receipts []receipt.Receipt) []receiptSummary {
	out := make([]receiptSummary, 0, len(receipts))
	for _, r := range receipts {
		out = append(out, receiptSummary{
			Fingerprint: r.Fingerprint.Hash(),
			Lane:        r.Fingerprint.Lane,
			SourceRunID: r.SourceRunID,
			CompletedAt: r.CompletedAt,
		})
	}
	return out
}
