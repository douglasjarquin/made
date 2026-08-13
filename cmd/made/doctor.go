package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/gitgate"
	"github.com/douglasjarquin/made/internal/herdrclient"
)

const doctorCheckTimeout = 5 * time.Second

func runDoctorCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		_, _ = fmt.Fprintln(stderr, "usage: made doctor [path]")
		return 2
	}
	targetPath := "."
	if fs.NArg() == 1 {
		targetPath = fs.Arg(0)
	}

	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made doctor:", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), doctorCheckTimeout)
	defer cancel()

	healthy := true

	if err := checkDaemon(api.SocketPath(home)); err != nil {
		_, _ = fmt.Fprintf(stdout, "daemon: unreachable (%v)\n", err)
		healthy = false
	} else {
		_, _ = fmt.Fprintln(stdout, "daemon: reachable")
	}

	ghClient := &github.Client{Timeout: doctorCheckTimeout}
	if err := ghClient.AuthStatus(ctx); err != nil {
		_, _ = fmt.Fprintf(stdout, "gh: not authenticated (%v)\n", err)
		healthy = false
	} else {
		_, _ = fmt.Fprintln(stdout, "gh: authenticated")
	}

	herdrResult := herdrclient.Connect(ctx)
	_, _ = fmt.Fprintf(stdout, "herdr: %s (informational only)\n", herdrResult.State)

	if gatePath, err := resolveGatePath(home, targetPath); err == nil && gateInitialized(gatePath) {
		_, _ = fmt.Fprintln(stdout, "gate: initialized")
	} else {
		_, _ = fmt.Fprintln(stdout, "gate: not initialized (run made gate init)")
	}

	if !healthy {
		return 1
	}
	return 0
}

// resolveGatePath mirrors gateInit's target-path resolution (gate.go) so
// doctor's gate check resolves to the exact same bare-repo path a prior
// `made gate init` for the same directory would have created.
func resolveGatePath(madeHomeDir, targetPath string) (string, error) {
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("resolve target path %s: %w", targetPath, err)
	}
	if resolved, err := filepath.EvalSymlinks(absTarget); err == nil {
		absTarget = resolved
	}
	return gitgate.GatePath(madeHomeDir, absTarget), nil
}

func gateInitialized(barePath string) bool {
	info, err := os.Stat(barePath)
	return err == nil && info.IsDir()
}

func checkDaemon(socketPath string) error {
	client, err := api.Dial(socketPath)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	_, err = client.Call("ping", nil)
	return err
}
