package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/verify"
)

const verifyCommandTimeout = 2 * time.Hour

func runVerifyCommand(args []string, stdout, stderr *os.File) int {
	if len(args) == 0 {
		return runVerifyRunCommand(args, stdout, stderr)
	}
	switch args[0] {
	case "run":
		return runVerifyRunCommand(args[1:], stdout, stderr)
	case "prepare":
		return runVerifyPrepareCommand(args[1:], stdout, stderr)
	case "complete":
		return runVerifyCompleteCommand(args[1:], stdout, stderr)
	case "status":
		return runVerifyStatusCommand(args[1:], stdout, stderr)
	case "receipt":
		return runVerifyReceiptCommand(args[1:], stdout, stderr)
	case "clean":
		return runVerifyCleanCommand(args[1:], stdout, stderr)
	default:
		return runVerifyRunCommand(args, stdout, stderr)
	}
}

func runVerifyRunCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made verify run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	baseRef := fs.String("base-ref", "", "local base ref to resolve, e.g. origin/main")
	repoPath := fs.String("repo", ".", "path to the repository to verify")
	reviewSource := fs.String("review-source", managed.ReviewSourceInternal, "review source for the one-shot flow; only internal is supported here")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made verify [run] --base-ref <ref> [--json]")
		return 2
	}
	if *reviewSource != managed.ReviewSourceInternal {
		_, _ = fmt.Fprintf(stderr, "made verify: --review-source %q is not supported by the one-shot flow; use made verify prepare/complete for external review\n", *reviewSource)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), verifyCommandTimeout)
	defer cancel()

	out, err := verify.Run(ctx, verify.RunParams{WorkDir: *repoPath, BaseRef: *baseRef})
	if err != nil {
		return failVerify(stdout, stderr, *asJSON, "made verify", err)
	}

	if *asJSON {
		return emitJSON(stdout, stderr, out.Receipt, out.Receipt.Outcome.ExitCode())
	}
	printHumanReceipt(stdout, out.Receipt, out.Engine.EvidenceDir)
	return out.Receipt.Outcome.ExitCode()
}

func runVerifyPrepareCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made verify prepare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	baseRef := fs.String("base-ref", "", "local base ref to resolve, e.g. origin/main")
	repoPath := fs.String("repo", ".", "path to the repository to verify")
	executor := fs.String("executor", "", "the external reviewer that will consume this request, e.g. cursor")
	requestedModel := fs.String("requested-model", "", "optional preferred reviewer model (provenance only)")
	output := fs.String("output", "", "path to write the review request to (a safe temp path is chosen when omitted)")
	taskFile := fs.String("task-file", "", "optional bounded task/acceptance-context file to embed in the request")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made verify prepare --executor <name> --base-ref <ref> [--output <path>] [--json]")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), verifyCommandTimeout)
	defer cancel()

	out, err := verify.Prepare(ctx, verify.PrepareParams{
		WorkDir:        *repoPath,
		BaseRef:        *baseRef,
		Executor:       *executor,
		RequestedModel: *requestedModel,
		Output:         *output,
		TaskFile:       *taskFile,
	})
	if err != nil {
		return failVerify(stdout, stderr, *asJSON, "made verify prepare", err)
	}

	if *asJSON {
		return emitJSON(stdout, stderr, prepareReport{
			RequestPath:  out.RequestPath,
			ContractHash: out.Request.ContractHash,
			BaseSHA:      out.Request.Contract.BaseSHA,
			InputSHA:     out.Request.Contract.InputSHA,
			ConfigPath:   out.Request.Config.Path,
			ConfigHash:   out.Request.Config.Hash,
		}, 0)
	}
	_, _ = fmt.Fprintf(stdout, "Review request written to %s\n", out.RequestPath)
	_, _ = fmt.Fprintf(stdout, "Input SHA:      %s\n", out.Request.Contract.InputSHA)
	_, _ = fmt.Fprintf(stdout, "Base SHA:       %s\n", out.Request.Contract.BaseSHA)
	_, _ = fmt.Fprintf(stdout, "Config:         %s (%s)\n", out.Request.Config.Path, out.Request.Config.Hash)
	_, _ = fmt.Fprintf(stdout, "Contract hash:  %s\n", out.Request.ContractHash)
	if out.Context.Warning != "" {
		_, _ = fmt.Fprintf(stdout, "Warning:        %s\n", out.Context.Warning)
	}
	_, _ = fmt.Fprintln(stdout, "Next: launch your external reviewer against this request, then run made verify complete.")
	return 0
}

type prepareReport struct {
	RequestPath  string `json:"request_path"`
	ContractHash string `json:"contract_hash"`
	BaseSHA      string `json:"base_sha"`
	InputSHA     string `json:"input_sha"`
	ConfigPath   string `json:"config_path"`
	ConfigHash   string `json:"config_hash"`
}

func runVerifyCompleteCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made verify complete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	repoPath := fs.String("repo", ".", "path to the repository to verify")
	requestPath := fs.String("request", "", "path to the review request written by made verify prepare")
	reviewResult := fs.String("review-result", "", "path to the external reviewer's result file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made verify complete --request <path> --review-result <path> [--json]")
		return 2
	}
	if *requestPath == "" {
		_, _ = fmt.Fprintln(stderr, "made verify complete: --request is required")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), verifyCommandTimeout)
	defer cancel()

	out, err := verify.Complete(ctx, verify.CompleteParams{
		WorkDir:          *repoPath,
		RequestPath:      *requestPath,
		ReviewResultPath: *reviewResult,
	})
	if err != nil {
		return failVerify(stdout, stderr, *asJSON, "made verify complete", err)
	}

	if *asJSON {
		return emitJSON(stdout, stderr, out.Receipt, out.Receipt.Outcome.ExitCode())
	}
	printHumanReceipt(stdout, out.Receipt, out.Engine.EvidenceDir)
	return out.Receipt.Outcome.ExitCode()
}

func runVerifyStatusCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made verify status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	repoPath := fs.String("repo", ".", "path to the repository")
	head := fs.Bool("head", true, "report the receipt for current HEAD")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || !*head {
		_, _ = fmt.Fprintln(stderr, "usage: made verify status --head [--json]")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), verifyCommandTimeout)
	defer cancel()

	res, err := verify.StatusHead(ctx, *repoPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made verify status:", err)
		return 1
	}

	if *asJSON {
		return emitJSON(stdout, stderr, statusReport{
			Repository: res.Repository,
			InputSHA:   res.InputSHA,
			Found:      res.Receipt != nil,
			Receipt:    res.Receipt,
		}, 0)
	}
	if res.Receipt == nil {
		_, _ = fmt.Fprintf(stdout, "No receipt found for current HEAD (%s).\n", res.InputSHA)
		return 0
	}
	printHumanReceipt(stdout, *res.Receipt, "")
	return 0
}

type statusReport struct {
	Repository string          `json:"repository"`
	InputSHA   string          `json:"input_sha"`
	Found      bool            `json:"found"`
	Receipt    *verify.Receipt `json:"receipt,omitempty"`
}

func runVerifyReceiptCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made verify receipt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	repoPath := fs.String("repo", ".", "path to the repository")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: made verify receipt [--json] [--repo <path>] <input-sha>")
		return 2
	}
	inputSHA := fs.Arg(0)

	ctx, cancel := context.WithTimeout(context.Background(), verifyCommandTimeout)
	defer cancel()

	r, ok, err := verify.ReceiptForSHA(ctx, *repoPath, inputSHA)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made verify receipt:", err)
		return 1
	}

	if *asJSON {
		return emitJSON(stdout, stderr, statusReport{InputSHA: inputSHA, Found: ok, Receipt: receiptOrNil(r, ok)}, 0)
	}
	if !ok {
		_, _ = fmt.Fprintf(stdout, "No receipt found for %s.\n", inputSHA)
		return 0
	}
	printHumanReceipt(stdout, r, "")
	return 0
}

func receiptOrNil(r verify.Receipt, ok bool) *verify.Receipt {
	if !ok {
		return nil
	}
	return &r
}

func runVerifyCleanCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made verify clean", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoPath := fs.String("repo", ".", "path to the repository")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made verify clean")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), verifyCommandTimeout)
	defer cancel()

	dir, err := verify.Clean(ctx, *repoPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made verify clean:", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "Removed %s\n", dir)
	return 0
}

// jsonErrorEnvelope is the bounded JSON document written to stdout when
// made verify [run]/prepare/complete --json fails before it can build its
// normal success payload (an Outcome doesn't exist yet at that point), so a
// caller parsing --json output always gets valid JSON on stdout, never a
// bare stderr line.
type jsonErrorEnvelope struct {
	Error    string `json:"error"`
	ExitCode int    `json:"exit_code"`
}

// failVerify reports a pre-Outcome failure (config-locate, guide resolution,
// HEAD/worktree/contract drift, etc.). Human-mode stderr output is unchanged;
// --json additionally gets a parseable error envelope on stdout, mapped to
// infrastructure_error's exit code since these failures are exactly that
// class in internal/managed's own classification.
func failVerify(stdout, stderr *os.File, asJSON bool, prefix string, err error) int {
	message := fmt.Sprintf("%s: %s", prefix, err)
	_, _ = fmt.Fprintln(stderr, message)
	exitCode := managed.OutcomeInfrastructureError.ExitCode()
	if asJSON {
		_ = json.NewEncoder(stdout).Encode(jsonErrorEnvelope{Error: message, ExitCode: exitCode})
	}
	return exitCode
}

func emitJSON(stdout, stderr *os.File, v any, exitCode int) int {
	encoder := json.NewEncoder(stdout)
	if err := encoder.Encode(v); err != nil {
		_, _ = fmt.Fprintln(stderr, "made verify:", err)
		return 1
	}
	return exitCode
}

func printHumanReceipt(stdout *os.File, r verify.Receipt, evidenceDir string) {
	_, _ = fmt.Fprintf(stdout, "Outcome:   %s\n", r.Outcome)
	_, _ = fmt.Fprintf(stdout, "Input SHA: %s\n", r.InputSHA)
	_, _ = fmt.Fprintf(stdout, "Base SHA:  %s\n", r.BaseSHA)
	_, _ = fmt.Fprintf(stdout, "Config:    %s (%s)\n", r.Config.Path, r.Config.Hash)
	if r.Review != nil {
		_, _ = fmt.Fprintf(stdout, "Review:    source=%s executor=%s requested_model=%s actual_model=%s guides=%d\n",
			r.Review.Source, r.Review.Executor, r.Review.RequestedModel, r.Review.ActualModel, len(r.Review.Guides))
	}
	_, _ = fmt.Fprintln(stdout, "Stages:")
	for _, s := range r.Stages {
		_, _ = fmt.Fprintf(stdout, "  %-10s %s\n", s.Name, s.Status)
		if s.AgentResolution != nil {
			if s.AgentResolution.Selected != nil {
				_, _ = fmt.Fprintf(stdout, "             agent: %s (resolved)\n", *s.AgentResolution.Selected)
			} else {
				_, _ = fmt.Fprintf(stdout, "             agent: %s\n", formatDoctorAgentResolutionFailure(*s.AgentResolution))
			}
		}
	}
	if evidenceDir != "" {
		_, _ = fmt.Fprintf(stdout, "Evidence:  %s\n", evidenceDir)
	}
	_, _ = fmt.Fprintf(stdout, "Next:      %s\n", nextAction(r.Outcome))
}

func nextAction(outcome managed.Outcome) string {
	switch outcome {
	case managed.OutcomePassed:
		return "none - this exact input SHA has a passing receipt"
	case managed.OutcomeNeedsDecision:
		return "record a Decision for the reported finding(s) and verify again"
	case managed.OutcomeFailedRetryable:
		return "fix the reported failure and run made verify again"
	case managed.OutcomeFailedTerminal:
		return "address the blocking finding(s); this input SHA cannot pass as-is"
	case managed.OutcomeCanceled:
		return "run made verify again"
	default:
		return "resolve the infrastructure issue and run made verify again"
	}
}
