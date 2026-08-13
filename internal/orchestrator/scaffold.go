package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/evidence"
	execpkg "github.com/douglasjarquin/made/internal/exec"
	"github.com/douglasjarquin/made/internal/gitgate"
	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/pipeline"
)

const pushedConfigFileName = ".made.yml"

type RunContext struct {
	Config     config.Config
	Worktree   *gitgate.Worktree
	Visibility *pipeline.Visibility
	Evidence   evidence.Store
	GitHub     *github.Client
}

type WorkFunc func(ctx context.Context, rc *RunContext) error

// setupTestHook is a no-op seam only a white-box test overrides, to prove
// Setup's panic recovery below actually cleans up a worktree/pane created
// earlier in the same call rather than just a theoretical code path.
var setupTestHook = func() {}

func Setup(ctx context.Context, gatePath, defaultBranch, worktreesDir, runID, pushedSHA string) (rc *RunContext, err error) {
	var wt *gitgate.Worktree
	var vis *pipeline.Visibility

	// A single defer/recover covers every exit path (normal, error, and
	// panic) uniformly: recover() converts a panic into err, and the err
	// check right after cleans up whatever was already allocated.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("orchestrator: recovered panic during setup: %v", r)
		}
		if err != nil {
			if vis != nil {
				vis.Close(ctx)
			}
			if wt != nil {
				_ = wt.Remove()
			}
			rc = nil
		}
	}()

	wt, err = gitgate.AddWorktree(gatePath, worktreesDir, pushedSHA)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: cut worktree for %s: %w", pushedSHA, err)
	}

	vis = pipeline.Open(ctx, runID)

	cfg, err := resolveConfig(ctx, gatePath, defaultBranch, wt.Path)
	if err != nil {
		return nil, err
	}

	setupTestHook()

	store := evidence.NewStore(wt.Path, evidence.Config{
		StoreInRepo: cfg.Test.Evidence.StoreInRepo,
		Dir:         cfg.Test.Evidence.Dir,
		Branch:      cfg.Test.Evidence.Branch,
	})

	return &RunContext{
		Config:     cfg,
		Worktree:   wt,
		Visibility: vis,
		Evidence:   store,
		GitHub:     &github.Client{Dir: wt.Path},
	}, nil
}

func (rc *RunContext) Cleanup(ctx context.Context) {
	if rc == nil {
		return
	}
	if rc.Worktree != nil {
		_ = rc.Worktree.Remove()
	}
	rc.Visibility.Close(ctx)
}

func Run(ctx context.Context, gatePath, defaultBranch, worktreesDir, runID, pushedSHA string, work WorkFunc) (err error) {
	rc, err := Setup(ctx, gatePath, defaultBranch, worktreesDir, runID, pushedSHA)
	if err != nil {
		return err
	}
	defer rc.Cleanup(ctx)
	// Recover after Cleanup is deferred so work's panic still tears down
	// the worktree/pane, converting the panic into an error instead of
	// crashing the caller.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("orchestrator: recovered panic during run: %v", r)
		}
	}()

	return work(ctx, rc)
}

func resolveConfig(ctx context.Context, gatePath, defaultBranch, worktreePath string) (config.Config, error) {
	trustedPath, cleanup, err := extractTrustedConfig(ctx, gatePath, defaultBranch)
	if err != nil {
		return config.Config{}, err
	}
	if cleanup != nil {
		defer cleanup()
	}

	pushedPath := filepath.Join(worktreePath, pushedConfigFileName)
	if _, statErr := os.Stat(pushedPath); statErr != nil {
		pushedPath = ""
	}

	cfg, err := config.LoadEffectiveConfig(trustedPath, pushedPath)
	if err != nil {
		return config.Config{}, fmt.Errorf("orchestrator: load effective config: %w", err)
	}
	return cfg, nil
}

func extractTrustedConfig(ctx context.Context, gatePath, defaultBranch string) (path string, cleanup func(), err error) {
	res, err := execpkg.Run(ctx, execpkg.Command{
		Name: "git",
		Args: []string{"show", fmt.Sprintf("refs/heads/%s:%s", defaultBranch, pushedConfigFileName)},
		Dir:  gatePath,
	})
	if err != nil {
		return "", nil, fmt.Errorf("orchestrator: run git show for trusted config: %w", err)
	}
	// Non-zero exit means the ref or path doesn't exist - a fresh gate with
	// nothing fetched to its default branch yet, or no .made.yml on it. That
	// is a normal state, not an error: it must resolve to an empty trusted
	// path, not block the run.
	if res.ExitCode != 0 {
		return "", nil, nil
	}

	f, err := os.CreateTemp("", "made-trusted-config-*.yml")
	if err != nil {
		return "", nil, fmt.Errorf("orchestrator: create temp file for trusted config: %w", err)
	}
	if _, err := f.Write(res.Stdout); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("orchestrator: write trusted config temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("orchestrator: close trusted config temp file: %w", err)
	}

	name := f.Name()
	return name, func() { _ = os.Remove(name) }, nil
}
