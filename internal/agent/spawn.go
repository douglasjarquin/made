package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/douglasjarquin/made/internal/exec"
)

type SpawnParams struct {
	WorktreePath string
	BinaryPath   string
	ExtraEnv     []string
	Timeout      time.Duration
}

func Spawn(ctx context.Context, kind Kind, params SpawnParams) (Findings, error) {
	binary := params.BinaryPath
	if binary == "" {
		binary = kind.binaryName()
	}

	result, err := exec.Run(ctx, exec.Command{
		Name:    binary,
		Args:    []string{"review", "--worktree", params.WorktreePath},
		Dir:     params.WorktreePath,
		Env:     append(os.Environ(), params.ExtraEnv...),
		Timeout: params.Timeout,
	})
	if err != nil {
		return Findings{}, fmt.Errorf("agent: spawn %s (%s): %w", kind, binary, err)
	}
	if result.ExitCode != 0 {
		return Findings{}, fmt.Errorf("agent: %s (%s) exited %d: %s", kind, binary, result.ExitCode, result.Stderr)
	}

	var findings Findings
	if err := json.Unmarshal(result.Stdout, &findings); err != nil {
		return Findings{}, fmt.Errorf("agent: parse findings from %s: %w: stdout=%s", kind, err, result.Stdout)
	}
	return findings, nil
}
