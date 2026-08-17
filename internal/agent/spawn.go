package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/evidence"
	"github.com/douglasjarquin/made/internal/exec"
)

type SpawnParams struct {
	WorktreePath string
	BinaryPath   string
	ExtraEnv     []string
	Timeout      time.Duration
}

const defaultSpawnTimeout = 30 * time.Minute

const (
	reviewPreparationTimeout = 2 * time.Minute
	reviewPreparationLimit   = 1 << 20
)

func Spawn(ctx context.Context, kind Kind, params SpawnParams) (Findings, error) {
	binary := params.BinaryPath
	if binary == "" {
		binary = kind.binaryName()
	}

	reviewPath, cleanupReview, err := prepareReviewWorktree(ctx, params.WorktreePath)
	if err != nil {
		return Findings{}, fmt.Errorf("agent: prepare read-only review worktree: %w", err)
	}
	defer cleanupReview()

	args, cleanup, err := invocation(kind, reviewPath)
	if err != nil {
		return Findings{}, err
	}
	defer cleanup()
	timeout := params.Timeout
	if timeout <= 0 {
		timeout = defaultSpawnTimeout
	}
	result, err := exec.Run(ctx, exec.Command{
		Name:    binary,
		Args:    args,
		Dir:     reviewPath,
		Env:     reviewEnvironmentForDir(params.ExtraEnv, reviewPath),
		Stdin:   []byte("Return only the Made review JSON object matching the supplied schema.\n"),
		Timeout: timeout,
	})
	if err != nil {
		return Findings{}, fmt.Errorf("agent: spawn %s (%s): %w", kind, binary, err)
	}
	if result.ExitCode != 0 {
		return Findings{}, fmt.Errorf("agent: %s (%s) exited %d: %s", kind, binary, result.ExitCode, evidence.RedactString(string(result.Stderr)))
	}

	findings, err := decodeFindings(result.Stdout)
	if err != nil {
		return Findings{}, fmt.Errorf("agent: parse findings from %s: %w: stdout=%s", kind, err, evidence.RedactString(string(result.Stdout)))
	}
	return findings, nil
}

func prepareReviewWorktree(ctx context.Context, source string) (string, func(), error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return "", nil, fmt.Errorf("resolve source worktree: %w", err)
	}
	headResult, err := runReviewGit(ctx, source, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", nil, fmt.Errorf("read source HEAD: %w", err)
	}
	if headResult.ExitCode != 0 {
		return "", nil, commandFailure("read source HEAD", headResult)
	}
	head := strings.TrimSpace(string(headResult.Stdout))
	if head == "" {
		return "", nil, fmt.Errorf("read source HEAD returned an empty SHA")
	}

	tempRoot, err := os.MkdirTemp("", "made-review-worktree-")
	if err != nil {
		return "", nil, fmt.Errorf("create review worktree directory: %w", err)
	}
	reviewPath := filepath.Join(tempRoot, "repo")
	cleanupTemp := func() { _ = os.RemoveAll(tempRoot) }

	cloneResult, err := runReviewGit(ctx, "", "clone", "--no-local", "--no-hardlinks", "--no-checkout", source, reviewPath)
	if err != nil {
		cleanupTemp()
		return "", nil, fmt.Errorf("clone review worktree: %w", err)
	}
	if cloneResult.ExitCode != 0 {
		cleanupTemp()
		return "", nil, commandFailure("clone review worktree", cloneResult)
	}
	checkoutResult, err := runReviewGit(ctx, reviewPath, "checkout", "--detach", "--quiet", head)
	if err != nil {
		cleanupTemp()
		return "", nil, fmt.Errorf("checkout review HEAD: %w", err)
	}
	if checkoutResult.ExitCode != 0 {
		cleanupTemp()
		return "", nil, commandFailure("checkout review HEAD", checkoutResult)
	}
	clonedHead, err := runReviewGit(ctx, reviewPath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		cleanupTemp()
		return "", nil, fmt.Errorf("verify review HEAD: %w", err)
	}
	if clonedHead.ExitCode != 0 || strings.TrimSpace(string(clonedHead.Stdout)) != head {
		cleanupTemp()
		return "", nil, fmt.Errorf("review clone HEAD %q does not match source HEAD %q", strings.TrimSpace(string(clonedHead.Stdout)), head)
	}
	if err := rejectEscapingSymlinks(reviewPath); err != nil {
		cleanupTemp()
		return "", nil, fmt.Errorf("validate review worktree links: %w", err)
	}
	restoreModes, err := makeReviewTreeReadOnly(reviewPath)
	if err != nil {
		cleanupTemp()
		return "", nil, fmt.Errorf("make review worktree read-only: %w", err)
	}
	cleanup := func() {
		restoreModes()
		cleanupTemp()
	}
	return reviewPath, cleanup, nil
}

func runReviewGit(ctx context.Context, dir string, args ...string) (*exec.Result, error) {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	return exec.Run(ctx, exec.Command{
		Name:        "git",
		Args:        args,
		Env:         reviewEnvironmentForDir(nil, dir),
		Timeout:     reviewPreparationTimeout,
		OutputLimit: reviewPreparationLimit,
	})
}

func commandFailure(label string, result *exec.Result) error {
	return fmt.Errorf("%s exited %d: stdout=%s stderr=%s", label, result.ExitCode, evidence.RedactString(string(result.Stdout)), evidence.RedactString(string(result.Stderr)))
}

func rejectEscapingSymlinks(root string) error {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolve symlink %q: %w", path, err)
		}
		rel, err := filepath.Rel(root, target)
		if err != nil {
			return fmt.Errorf("relativize symlink %q: %w", path, err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("symlink %q escapes review worktree", path)
		}
		return nil
	})
}

func makeReviewTreeReadOnly(root string) (func(), error) {
	originalModes := make(map[string]os.FileMode)
	restore := func() {
		for path, mode := range originalModes {
			_ = os.Chmod(path, mode.Perm())
		}
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		originalModes[path] = info.Mode()
		mode := info.Mode().Perm() &^ 0o222
		if entry.IsDir() {
			mode = 0o555
		}
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		restore()
		return nil, err
	}
	return restore, nil
}

func reviewEnvironmentForDir(extra []string, dir string) []string {
	filtered := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && !sensitiveEnvironmentName(name) && !reviewPathEnvironmentName(name) && (dir == "" || name != "PWD") {
			filtered = append(filtered, entry)
		}
	}
	for _, entry := range extra {
		name, _, ok := strings.Cut(entry, "=")
		if ok && !sensitiveEnvironmentName(name) && !reviewPathEnvironmentName(name) && (dir == "" || name != "PWD") {
			filtered = append(filtered, entry)
		}
	}
	if dir != "" {
		filtered = append(filtered, "PWD="+dir)
	}
	return filtered
}

func reviewPathEnvironmentName(name string) bool {
	switch name {
	case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_QUARANTINE_PATH", "OLDPWD":
		return true
	default:
		return false
	}
}

func sensitiveEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	if upper == "SSH_AUTH_SOCK" || upper == "COOKIE" {
		return true
	}
	for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "PRIVATE_KEY", "CREDENTIAL"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

func invocation(kind Kind, worktree string) ([]string, func(), error) {
	if kind != KindCodex {
		return []string{"review", "--worktree", worktree}, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "made-codex-schema-")
	if err != nil {
		return nil, nil, fmt.Errorf("agent: create Codex schema directory: %w", err)
	}
	path := filepath.Join(dir, "output.json")
	if err := os.WriteFile(path, []byte(reviewSchema), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("agent: write Codex output schema: %w", err)
	}
	return []string{"exec", "--cd", worktree, "--json", "--output-schema", path, "-"}, func() { _ = os.RemoveAll(dir) }, nil
}

func decodeFindings(data []byte) (Findings, error) {
	var direct Findings
	if err := json.Unmarshal(data, &direct); err == nil {
		var envelope map[string]json.RawMessage
		if json.Unmarshal(data, &envelope) == nil {
			if raw, ok := envelope["findings"]; ok {
				if string(raw) == "null" {
					return Findings{Findings: []Finding{}}, nil
				}
				if values, err := strictFindings(data); err == nil {
					return values, nil
				}
			}
		}
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	var last string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "{") {
			var event struct {
				Item struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			}
			if json.Unmarshal([]byte(line), &event) == nil && event.Item.Type == "agent_message" {
				last = event.Item.Text
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Findings{}, err
	}
	if last == "" {
		return Findings{}, fmt.Errorf("structured findings payload was not found")
	}
	return strictFindings([]byte(last))
}

func strictFindings(data []byte) (Findings, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var findings Findings
	if err := decoder.Decode(&findings); err != nil {
		return Findings{}, err
	}
	for _, finding := range findings.Findings {
		if finding.Description == "" {
			return Findings{}, fmt.Errorf("finding description is required")
		}
		switch finding.Kind {
		case FindingAutoFixable:
			if strings.TrimSpace(finding.Patch) == "" {
				return Findings{}, fmt.Errorf("auto-fixable finding patch is required")
			}
			if len(finding.Paths) == 0 {
				return Findings{}, fmt.Errorf("auto-fixable finding paths are required")
			}
		case FindingAskUser, FindingBlocking:
		default:
			return Findings{}, fmt.Errorf("unknown finding kind %q", finding.Kind)
		}
	}
	return findings, nil
}

const reviewSchema = `{"type":"object","additionalProperties":false,"required":["findings"],"properties":{"findings":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["kind","description"],"properties":{"kind":{"type":"string","enum":["auto-fixable","ask-user","blocking"]},"description":{"type":"string"},"patch":{"type":"string"},"paths":{"type":"array","items":{"type":"string"}}}}}}}`
