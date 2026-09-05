package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/gitgate"
	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/herdrclient"
)

const doctorCheckTimeout = 5 * time.Second

func runDoctorCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
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
	if *asJSON {
		return runDoctorJSON(targetPath, stdout, stderr)
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

	_, _ = fmt.Fprintln(stdout, "agent:", doctorAgentResolutionSummary(ctx, targetPath))

	if !healthy {
		return 1
	}
	return 0
}

// doctorAgentResolutionSummary reports the effective resolved agent (or,
// on exhaustion, every candidate tried and why) without spawning any real
// review invocation - only the same read-only presence/auth/quota probing
// agent.Resolve already does (project: agent auto-resolve, brief item 5).
// Informational only: an unresolvable agent never flips doctor's overall
// Healthy status, since this is inspectability, not a health gate.
func doctorAgentResolutionSummary(ctx context.Context, targetPath string) string {
	loc, err := config.Locate(targetPath)
	if err != nil || loc.Layout == config.LayoutAbsent {
		return "not_configured (no .made.yaml or .made/config.yaml found)"
	}
	data, err := os.ReadFile(loc.Path)
	if err != nil {
		return fmt.Sprintf("unknown (%v)", err)
	}
	cfg, err := config.ParseBytes(data)
	if err != nil {
		return fmt.Sprintf("unknown (%v)", err)
	}
	if cfg.AgentIsPinned() {
		kind, err := cfg.AgentKind()
		if err != nil {
			return fmt.Sprintf("unknown (%v)", err)
		}
		return fmt.Sprintf("%s (pinned)", kind)
	}
	res := agent.Resolve(ctx, cfg.AgentCandidates())
	if res.Selected != nil {
		return fmt.Sprintf("%s (resolved)", *res.Selected)
	}
	return formatDoctorAgentResolutionFailure(res)
}

func formatDoctorAgentResolutionFailure(res agent.AgentResolution) string {
	parts := make([]string, 0, len(res.Attempts))
	for _, a := range res.Attempts {
		reason := string(a.Reason)
		if a.Reason == agent.ReasonQuotaExhausted && a.QuotaResetsAt != nil {
			reason = fmt.Sprintf("quota-exhausted-until-%s", a.QuotaResetsAt.Format(time.RFC3339))
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", a.Kind, reason))
	}
	return "none available: " + strings.Join(parts, ", ")
}

type doctorReport struct {
	SchemaVersion   int               `json:"schema_version"`
	ProtocolVersion int               `json:"protocol_version"`
	Healthy         bool              `json:"healthy"`
	Checks          map[string]string `json:"checks"`
}

func runDoctorJSON(targetPath string, stdout, stderr *os.File) int {
	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made doctor:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), doctorCheckTimeout)
	defer cancel()
	checks := make(map[string]string)
	healthy := true
	if err := checkDaemon(api.SocketPath(home)); err != nil {
		checks["daemon"] = "unreachable"
		healthy = false
	} else {
		checks["daemon"] = "reachable"
	}
	ghClient := &github.Client{Timeout: doctorCheckTimeout}
	if err := ghClient.AuthStatus(ctx); err != nil {
		checks["github"] = "unavailable"
		healthy = false
	} else {
		checks["github"] = "authenticated"
	}
	checks["herdr"] = herdrclient.Connect(ctx).State.String()
	if gatePath, gateErr := resolveGatePath(home, targetPath); gateErr == nil && gateInitialized(gatePath) {
		checks["gate"] = "initialized"
	} else {
		checks["gate"] = "not_initialized"
	}
	checks["agent_resolution"] = doctorAgentResolutionSummary(ctx, targetPath)
	encoder := json.NewEncoder(stdout)
	if err := encoder.Encode(doctorReport{SchemaVersion: 1, ProtocolVersion: api.Version, Healthy: healthy, Checks: checks}); err != nil {
		_, _ = fmt.Fprintln(stderr, "made doctor:", err)
		return 1
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
