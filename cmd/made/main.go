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
	case "capabilities":
		return runCapabilitiesCommand(args[1:], stdout, stderr)
	case "daemon":
		return runDaemonCommand(args[1:], stdout, stderr)
	case "run":
		return runRunCommand(args[1:], stdout, stderr)
	case "status":
		_, _ = fmt.Fprintln(stderr, "made: status is obsolete; use made run status <exact-run-id>")
		return 2
	case "review":
		return runReviewCommand(args[1:], os.Stdin, stdout, stderr)
	case "pr":
		return runPRCommand(args[1:], stdout, stderr)
	case "doctor":
		return runDoctorCommand(args[1:], stdout, stderr)
	case "gate":
		return runGateCommand(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "made: unknown command %q\n", args[0])
		return 2
	}
}
