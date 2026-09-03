package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/evidence"
	execpkg "github.com/douglasjarquin/made/internal/exec"
	"github.com/douglasjarquin/made/internal/gitgate"
	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/pipeline"
)

const pushedConfigFileName = ".made.yml"

const githubCallTimeout = 30 * time.Second

func gitEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=commit.gpgsign",
		"GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=safe.bareRepository",
		"GIT_CONFIG_VALUE_1=all",
	)
}

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
		StoreInRepo:    cfg.Test.Evidence.StoreInRepo,
		Dir:            cfg.Test.Evidence.Dir,
		Branch:         cfg.Test.Evidence.Branch,
		RetentionBytes: cfg.EvidenceRetentionBytes(),
	})

	return &RunContext{
		Config:     cfg,
		Worktree:   wt,
		Visibility: vis,
		Evidence:   store,
		GitHub:     &github.Client{Dir: wt.Path, Timeout: githubCallTimeout},
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
	if err := refreshDefaultBranch(ctx, gatePath, defaultBranch); err != nil {
		return config.Config{}, err
	}
	trustedPath, _, cleanup, err := extractTrustedConfig(ctx, gatePath, defaultBranch)
	if err != nil {
		return config.Config{}, fmt.Errorf("orchestrator: resolve trusted config: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	pushedLoc, err := config.Locate(worktreePath)
	if err != nil {
		return config.Config{}, fmt.Errorf("orchestrator: resolve candidate config: %w", err)
	}

	cfg, err := config.LoadEffectiveConfig(trustedPath, pushedLoc.Path)
	if err != nil {
		return config.Config{}, fmt.Errorf("orchestrator: load effective config: %w", err)
	}
	return cfg, nil
}

func refreshDefaultBranch(ctx context.Context, gatePath, defaultBranch string) error {
	remote, err := execpkg.Run(ctx, execpkg.Command{Name: "git", Args: []string{"remote", "get-url", "origin"}, Dir: gatePath, Env: gitEnv()})
	if err != nil {
		return fmt.Errorf("orchestrator: inspect origin remote: %w", err)
	}
	if remote.ExitCode != 0 {
		return fmt.Errorf("orchestrator: origin remote is unavailable: %s", string(remote.Stderr))
	}
	refspec := fmt.Sprintf("%s:refs/heads/%s", defaultBranch, defaultBranch)
	fetch, err := execpkg.Run(ctx, execpkg.Command{Name: "git", Args: []string{"fetch", "origin", refspec}, Dir: gatePath, Env: gitEnv()})
	if err != nil {
		return fmt.Errorf("orchestrator: refresh default branch %s: %w", defaultBranch, err)
	}
	if fetch.ExitCode != 0 {
		if strings.Contains(string(fetch.Stderr), "couldn't find remote ref") {
			clear, clearErr := execpkg.Run(ctx, execpkg.Command{
				Name: "git",
				Args: []string{"update-ref", "-d", "refs/heads/" + defaultBranch},
				Dir:  gatePath,
				Env:  gitEnv(),
			})
			if clearErr != nil {
				return fmt.Errorf("orchestrator: clear deleted default branch %s: %w", defaultBranch, clearErr)
			}
			if clear.ExitCode != 0 {
				return fmt.Errorf("orchestrator: clear deleted default branch %s failed: %s", defaultBranch, string(clear.Stderr))
			}
			return nil
		}
		return fmt.Errorf("orchestrator: refresh default branch %s failed: %s", defaultBranch, string(fetch.Stderr))
	}
	return nil
}

type trustedConfigCandidate struct {
	gitPath string
	layout  config.Layout
}

var trustedConfigCandidates = []trustedConfigCandidate{
	{config.RootConfigFileName, config.LayoutRoot},
	{config.DirectoryConfigRelPath, config.LayoutDirectory},
	{pushedConfigFileName, config.LayoutLegacy},
}

// Mirrors config.Locate's conflict rules but reads the git tree directly, since the trusted copy must never be affected by anything the pushed branch controls.
func locateTrustedConfig(ctx context.Context, gatePath, ref string) (gitPath string, layout config.Layout, exists bool, err error) {
	var present []trustedConfigCandidate
	for _, candidate := range trustedConfigCandidates {
		res, runErr := execpkg.Run(ctx, execpkg.Command{
			Name: "git",
			Args: []string{"cat-file", "-e", fmt.Sprintf("%s:%s", ref, candidate.gitPath)},
			Dir:  gatePath,
		})
		if runErr != nil {
			return "", "", false, fmt.Errorf("orchestrator: check trusted config path %q: %w", candidate.gitPath, runErr)
		}
		if res.ExitCode == 0 {
			present = append(present, candidate)
		}
	}
	switch len(present) {
	case 0:
		return "", "", false, nil
	case 1:
		return present[0].gitPath, present[0].layout, true, nil
	default:
		paths := make([]string, len(present))
		for i, candidate := range present {
			paths[i] = candidate.gitPath
		}
		return "", config.LayoutConflict, false, &config.ConflictError{Root: ref, Paths: paths}
	}
}

func extractTrustedConfig(ctx context.Context, gatePath, defaultBranch string) (path string, layout config.Layout, cleanup func(), err error) {
	ref := fmt.Sprintf("refs/heads/%s", defaultBranch)
	gitPath, foundLayout, exists, err := locateTrustedConfig(ctx, gatePath, ref)
	if err != nil {
		return "", "", nil, err
	}
	if !exists {
		return "", "", nil, nil
	}

	res, err := execpkg.Run(ctx, execpkg.Command{
		Name: "git",
		Args: []string{"show", fmt.Sprintf("%s:%s", ref, gitPath)},
		Dir:  gatePath,
		Env:  gitEnv(),
	})
	if err != nil {
		return "", "", nil, fmt.Errorf("orchestrator: run git show for trusted config: %w", err)
	}
	if res.ExitCode != 0 {
		return "", "", nil, fmt.Errorf("orchestrator: trusted config %q disappeared between check and extraction", gitPath)
	}

	tmpDir, err := os.MkdirTemp("", "made-trusted-config-")
	if err != nil {
		return "", "", nil, fmt.Errorf("orchestrator: create temp dir for trusted config: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(tmpDir) }

	destPath := filepath.Join(tmpDir, filepath.FromSlash(gitPath))
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("orchestrator: create trusted config temp dir tree: %w", err)
	}
	if err := os.WriteFile(destPath, res.Stdout, 0o600); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("orchestrator: write trusted config temp file: %w", err)
	}

	return destPath, foundLayout, cleanup, nil
}
