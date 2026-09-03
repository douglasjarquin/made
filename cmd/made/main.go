package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	if len(args) < 1 {
		_, _ = fmt.Fprintln(stderr, "usage: made <command> [args]")
		return 2
	}

	switch args[0] {
	case "validate":
		return runValidateCommand(args[1:], stdout, stderr)
	case "capabilities":
		return runCapabilitiesCommand(args[1:], stdout, stderr)
	case "run":
		return runRunCommand(args[1:], stdout, stderr)
	case "status":
		_, _ = fmt.Fprintln(stderr, "made: status is obsolete; use made run status --json <exact-run-id>")
		return 2
	case "daemon":
		return runDaemonCommand(args[1:], stdout, stderr)
	case "review":
		if len(args) > 1 && args[1] == "decide" {
			return runReviewDecideCommand(args[2:], stdout, stderr)
		}
		_, _ = fmt.Fprintln(stderr, "made review: use the versioned decide subcommand")
		return 2
	case "doctor":
		return runDoctorCommand(args[1:], stdout, stderr)
	case "plan":
		return runPlanCommand(args[1:], stdout, stderr)
	case "gate":
		return runGateCommand(args[1:], stdout, stderr)
	case "receipts":
		return runReceiptsCommand(args[1:], stdout, stderr)
	case "config":
		return runConfigCommand(args[1:], stdout, stderr)
	case "verify":
		return runVerifyCommand(args[1:], stdout, stderr)
	case "cursor":
		return runCursorCommand(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "made: unknown command %q\n", args[0])
		return 2
	}
}
