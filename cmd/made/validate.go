package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/douglasjarquin/made/internal/managed"
)

// runValidateCommand is the entry point for `made validate --managed --json-events ...`.
func runValidateCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made validate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	managedMode := fs.Bool("managed", false, "run in managed-validation mode")
	jsonEvents := fs.Bool("json-events", false, "emit JSON-Lines events to stdout")

	runID := fs.String("run-id", "", "opaque run identifier echoed in every event")
	missionID := fs.String("mission-id", "", "opaque mission identifier echoed in every event")
	workspace := fs.String("workspace", "", "absolute path to Git working tree")
	baseSHA := fs.String("base-sha", "", "full 40-hex base commit SHA")
	inputSHA := fs.String("input-sha", "", "full 40-hex input commit SHA (must equal workspace HEAD)")
	trustedConfig := fs.String("trusted-config", "", "absolute path to trusted .made.yml")
	policyHash := fs.String("policy-hash", "", "sha256:<64-hex> of trusted-config bytes")
	evidenceDir := fs.String("evidence-dir", "", "absolute path outside workspace for evidence output")
	decisions := fs.String("decisions", "", "optional absolute path to Decisions JSON file")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if !*managedMode {
		_, _ = fmt.Fprintln(stderr, "made validate: --managed is required")
		return 2
	}
	if !*jsonEvents {
		_, _ = fmt.Fprintln(stderr, "made validate: --json-events is required")
		return 2
	}

	// Validate required flags.
	missing := []string{}
	if *runID == "" {
		missing = append(missing, "--run-id")
	}
	if *missionID == "" {
		missing = append(missing, "--mission-id")
	}
	if *workspace == "" {
		missing = append(missing, "--workspace")
	}
	if *baseSHA == "" {
		missing = append(missing, "--base-sha")
	}
	if *inputSHA == "" {
		missing = append(missing, "--input-sha")
	}
	if *trustedConfig == "" {
		missing = append(missing, "--trusted-config")
	}
	if *policyHash == "" {
		missing = append(missing, "--policy-hash")
	}
	if *evidenceDir == "" {
		missing = append(missing, "--evidence-dir")
	}
	if len(missing) > 0 {
		for _, flag := range missing {
			_, _ = fmt.Fprintf(stderr, "made validate: missing required flag %s\n", flag)
		}
		return 2
	}

	opts := &managed.Options{
		RunID:         *runID,
		MissionID:     *missionID,
		Workspace:     *workspace,
		BaseSHA:       *baseSHA,
		InputSHA:      *inputSHA,
		TrustedConfig: *trustedConfig,
		PolicyHash:    *policyHash,
		EvidenceDir:   *evidenceDir,
		DecisionsPath: *decisions,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return managed.Run(ctx, opts, stdout, stderr)
}
