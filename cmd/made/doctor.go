package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/gitgate"
	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/herdrclient"
)

const doctorCheckTimeout = 5 * time.Second

type doctorReport struct {
	SchemaVersion   int               `json:"schema_version"`
	ProtocolVersion int               `json:"protocol_version"`
	Healthy         bool              `json:"healthy"`
	Checks          map[string]string `json:"checks"`
}

func runDoctorCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "output structured JSON")
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

	daemonErr := checkDaemon(api.SocketPath(home))

	ghClient := &github.Client{Timeout: doctorCheckTimeout}
	githubErr := ghClient.AuthStatus(ctx)
	healthy := daemonErr == nil && githubErr == nil

	herdrResult := herdrclient.Connect(ctx)

	gateState := "not_initialized"

	if gatePath, err := resolveGatePath(home, targetPath); err == nil && gateInitialized(gatePath) {
		gateState = "initialized"
	}

	if *jsonOutput {
		checks := map[string]string{
			"daemon": "reachable",
			"github": "authenticated",
			"herdr":  herdrResult.State.String(),
			"gate":   gateState,
		}
		if daemonErr != nil {
			checks["daemon"] = "unreachable"
		}
		if githubErr != nil {
			checks["github"] = "unavailable"
		}
		report := doctorReport{
			SchemaVersion:   1,
			ProtocolVersion: api.Version,
			Healthy:         healthy,
			Checks:          checks,
		}
		if err := json.NewEncoder(stdout).Encode(report); err != nil {
			_, _ = fmt.Fprintln(stderr, "made doctor:", err)
			return 1
		}
		if !healthy {
			return 1
		}
		return 0
	}

	if daemonErr != nil {
		_, _ = fmt.Fprintf(stdout, "daemon: unreachable (%v)\n", daemonErr)
	} else {
		_, _ = fmt.Fprintln(stdout, "daemon: reachable")
	}
	if githubErr != nil {
		_, _ = fmt.Fprintf(stdout, "gh: not authenticated (%v)\n", githubErr)
	} else {
		_, _ = fmt.Fprintln(stdout, "gh: authenticated")
	}
	_, _ = fmt.Fprintf(stdout, "herdr: %s (informational only)\n", herdrResult.State.String())

	if gateState == "initialized" {
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
