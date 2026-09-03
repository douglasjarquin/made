package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/douglasjarquin/made/internal/config"
)

func runConfigCommand(args []string, stdout, stderr *os.File) int {
	if len(args) < 1 {
		_, _ = fmt.Fprintln(stderr, "usage: made config <path|check|move> [args]")
		return 2
	}
	switch args[0] {
	case "path":
		return runConfigPathCommand(args[1:], stdout, stderr)
	case "check":
		return runConfigCheckCommand(args[1:], stdout, stderr)
	case "move":
		return runConfigMoveCommand(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "made config: unknown subcommand %q\n", args[0])
		return 2
	}
}

type configPathReport struct {
	SchemaVersion int    `json:"schema_version"`
	Layout        string `json:"layout"`
	Path          string `json:"path,omitempty"`
	Warning       string `json:"warning,omitempty"`
	Error         string `json:"error,omitempty"`
}

func runConfigPathCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made config path", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	repoPath := fs.String("repo", ".", "path to the repository to inspect")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made config path [--json] [--repo <path>]")
		return 2
	}

	root, err := filepath.Abs(*repoPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made config path:", err)
		return 1
	}

	loc, locErr := config.Locate(root)
	report := configPathReport{SchemaVersion: 1, Layout: string(loc.Layout), Path: loc.Path, Warning: loc.Warning}
	if locErr != nil {
		report.Error = locErr.Error()
	}

	if *asJSON {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			_, _ = fmt.Fprintln(stderr, "made config path:", err)
			return 1
		}
	} else if locErr != nil {
		_, _ = fmt.Fprintln(stderr, "made config path:", locErr)
	} else {
		_, _ = fmt.Fprintf(stdout, "layout: %s\n", loc.Layout)
		if loc.Path != "" {
			_, _ = fmt.Fprintf(stdout, "path:   %s\n", loc.Path)
		}
		if loc.Warning != "" {
			_, _ = fmt.Fprintf(stdout, "warning: %s\n", loc.Warning)
		}
	}

	if locErr != nil {
		return 1
	}
	return 0
}

type configCheckReport struct {
	SchemaVersion int    `json:"schema_version"`
	Layout        string `json:"layout"`
	Path          string `json:"path,omitempty"`
	Valid         bool   `json:"valid"`
	Error         string `json:"error,omitempty"`
}

func runConfigCheckCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made config check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	repoPath := fs.String("repo", ".", "path to the repository to inspect")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made config check [--json] [--repo <path>]")
		return 2
	}

	root, err := filepath.Abs(*repoPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made config check:", err)
		return 1
	}

	loc, locErr := config.Locate(root)
	report := configCheckReport{SchemaVersion: 1, Layout: string(loc.Layout), Path: loc.Path}
	if locErr != nil {
		report.Error = locErr.Error()
	} else if loc.Layout != config.LayoutAbsent {
		if _, cfgErr := config.LoadEffectiveConfig(loc.Path, ""); cfgErr != nil {
			report.Error = cfgErr.Error()
		} else {
			report.Valid = true
		}
	} else {
		report.Valid = true
	}

	if *asJSON {
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			_, _ = fmt.Fprintln(stderr, "made config check:", err)
			return 1
		}
	} else if report.Error != "" {
		_, _ = fmt.Fprintln(stderr, "made config check:", report.Error)
	} else {
		_, _ = fmt.Fprintf(stdout, "layout: %s\nvalid:  true\n", loc.Layout)
	}

	if report.Error != "" {
		return 1
	}
	return 0
}

func runConfigMoveCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made config move", flag.ContinueOnError)
	fs.SetOutput(stderr)
	to := fs.String("to", "", "target layout: root or directory")
	repoPath := fs.String("repo", ".", "path to the repository to modify")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made config move --to root|directory [--repo <path>]")
		return 2
	}

	var target config.Layout
	switch *to {
	case "root":
		target = config.LayoutRoot
	case "directory":
		target = config.LayoutDirectory
	default:
		_, _ = fmt.Fprintln(stderr, "made config move: --to must be \"root\" or \"directory\"")
		return 2
	}

	root, err := filepath.Abs(*repoPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made config move:", err)
		return 1
	}

	from, dest, err := config.Move(root, target)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made config move:", err)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "moved %s to %s\n", from, dest)
	return 0
}
