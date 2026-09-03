package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/cursor"
)

const cursorCommandTimeout = 30 * time.Second

func runCursorCommand(args []string, stdout, stderr *os.File) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made cursor <init|sync|check|doctor> [args]")
		return 2
	}
	if isHelp(args[0]) {
		return printHelp(stdout, "usage: made cursor <init|sync|check|doctor> [args]")
	}
	switch args[0] {
	case "init":
		return runCursorInitCommand(args[1:], stdout, stderr)
	case "sync":
		return runCursorSyncCommand(args[1:], stdout, stderr)
	case "check":
		return runCursorCheckCommand(args[1:], stdout, stderr)
	case "doctor":
		return runCursorDoctorCommand(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "made cursor: unknown subcommand %q\n", args[0])
		return 2
	}
}

func loadCursorConfig(repoPath string) (root string, cfg config.Config, err error) {
	root, err = filepath.Abs(repoPath)
	if err != nil {
		return "", config.Config{}, err
	}
	loc, locErr := config.Locate(root)
	if locErr != nil {
		return root, config.Config{}, locErr
	}
	if loc.Layout == config.LayoutAbsent {
		return root, config.Config{}, nil
	}
	data, readErr := os.ReadFile(loc.Path)
	if readErr != nil {
		return root, config.Config{}, readErr
	}
	cfg, err = config.ParseBytes(data)
	return root, cfg, err
}

type cursorWriteReport struct {
	SchemaVersion int                  `json:"schema_version"`
	Results       []cursor.WriteResult `json:"results"`
}

func runCursorInitCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made cursor init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	repoPath := fs.String("repo", ".", "path to the repository")
	adopt := fs.Bool("adopt", false, "take ownership of an existing unmanaged file at a Made-owned path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made cursor init [--adopt] [--json]")
		return 2
	}

	root, cfg, err := loadCursorConfig(*repoPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made cursor init:", err)
		return 1
	}
	results, err := cursor.Init(root, cfg, *adopt)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made cursor init:", err)
		return 1
	}
	return reportCursorWrite(stdout, stderr, *asJSON, results)
}

func runCursorSyncCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made cursor sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	repoPath := fs.String("repo", ".", "path to the repository")
	adopt := fs.Bool("adopt", false, "take ownership of an existing unmanaged file at a Made-owned path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made cursor sync [--adopt] [--json]")
		return 2
	}

	root, cfg, err := loadCursorConfig(*repoPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made cursor sync:", err)
		return 1
	}
	results, err := cursor.Sync(root, cfg, *adopt)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made cursor sync:", err)
		return 1
	}
	return reportCursorWrite(stdout, stderr, *asJSON, results)
}

func reportCursorWrite(stdout, stderr *os.File, asJSON bool, results []cursor.WriteResult) int {
	if asJSON {
		if err := json.NewEncoder(stdout).Encode(cursorWriteReport{SchemaVersion: 1, Results: results}); err != nil {
			_, _ = fmt.Fprintln(stderr, "made cursor:", err)
			return 1
		}
		return 0
	}
	for _, r := range results {
		_, _ = fmt.Fprintf(stdout, "%-10s %s\n", r.Action, r.RelPath)
	}
	return 0
}

type cursorCheckReport struct {
	SchemaVersion int            `json:"schema_version"`
	Drifted       bool           `json:"drifted"`
	Drift         []cursor.Drift `json:"drift"`
}

func runCursorCheckCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made cursor check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	repoPath := fs.String("repo", ".", "path to the repository")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made cursor check [--json]")
		return 2
	}

	root, cfg, err := loadCursorConfig(*repoPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made cursor check:", err)
		return 1
	}
	drift, err := cursor.Check(root, cfg)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made cursor check:", err)
		return 1
	}

	if *asJSON {
		if err := json.NewEncoder(stdout).Encode(cursorCheckReport{SchemaVersion: 1, Drifted: len(drift) > 0, Drift: drift}); err != nil {
			_, _ = fmt.Fprintln(stderr, "made cursor check:", err)
			return 1
		}
		if len(drift) > 0 {
			return 1
		}
		return 0
	}

	if len(drift) == 0 {
		_, _ = fmt.Fprintln(stdout, "cursor projections are current")
		return 0
	}
	for _, d := range drift {
		_, _ = fmt.Fprintf(stdout, "%-20s %-8s %s\n", d.RelPath, d.Kind, d.Remediation)
	}
	return 1
}

type cursorDoctorReport struct {
	SchemaVersion int                  `json:"schema_version"`
	Healthy       bool                 `json:"healthy"`
	Checks        []cursor.DoctorCheck `json:"checks"`
}

func runCursorDoctorCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made cursor doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	repoPath := fs.String("repo", ".", "path to the repository")
	baseRef := fs.String("base-ref", "", "optional local base ref to confirm is resolvable, e.g. origin/main")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made cursor doctor [--base-ref <ref>] [--json]")
		return 2
	}

	root, err := filepath.Abs(*repoPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made cursor doctor:", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), cursorCommandTimeout)
	defer cancel()

	report := cursor.Doctor(ctx, cursor.DoctorParams{Root: root, BaseRef: *baseRef})

	if *asJSON {
		if err := json.NewEncoder(stdout).Encode(cursorDoctorReport{SchemaVersion: 1, Healthy: report.Healthy, Checks: report.Checks}); err != nil {
			_, _ = fmt.Fprintln(stderr, "made cursor doctor:", err)
			return 1
		}
	} else {
		for _, c := range report.Checks {
			_, _ = fmt.Fprintf(stdout, "%-18s %-8s %s\n", c.Name, c.Status, c.Detail)
		}
	}
	if !report.Healthy {
		return 1
	}
	return 0
}
