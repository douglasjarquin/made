package evidence

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
)

type OrphanBranchStore struct {
	RepoPath string
	Branch   string
}

// Location names where a run's evidence commit lives on the orphan branch,
// in the same "refs/heads/<branch>:<path>" shorthand git itself uses (e.g.
// `git show`), so callers get a reference they can act on directly rather
// than just the bare runID.
func (s *OrphanBranchStore) Location(runID string) string {
	branch := s.Branch
	if branch == "" {
		branch = DefaultBranch
	}
	return "refs/heads/" + branch + ":" + runID
}

// Builds the commit via plumbing (hash-object/write-tree/commit-tree) into a
// scratch GIT_INDEX_FILE instead of `git checkout --orphan`, so the target
// repo's real HEAD/index/working tree are never touched; parentless
// commit-tree is what gives the branch no shared history with the default
// branch.
func (s *OrphanBranchStore) WriteEvidence(runID string, files map[string][]byte) error {
	if runID == "" {
		return fmt.Errorf("evidence: runID must not be empty")
	}
	branch := s.Branch
	if branch == "" {
		branch = DefaultBranch
	}
	ref := "refs/heads/" + branch

	idxDir, err := os.MkdirTemp("", "made-evidence-idx-")
	if err != nil {
		return fmt.Errorf("evidence: create scratch index dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(idxDir) }()
	indexEnv := []string{"GIT_INDEX_FILE=" + idxDir + "/index"}

	parent, err := s.runGit(nil, nil, "rev-parse", "--verify", ref)
	hasParent := err == nil
	if hasParent {
		if _, err := s.runGit(indexEnv, nil, "read-tree", parent); err != nil {
			return fmt.Errorf("evidence: seed scratch index from existing evidence branch: %w", err)
		}
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		blobSHA, err := s.runGit(indexEnv, files[name], "hash-object", "-w", "--stdin")
		if err != nil {
			return fmt.Errorf("evidence: hash evidence file %q: %w", name, err)
		}
		entryPath := path.Join(runID, name)
		if _, err := s.runGit(indexEnv, nil, "update-index", "--add", "--cacheinfo", "100644,"+blobSHA+","+entryPath); err != nil {
			return fmt.Errorf("evidence: stage evidence file %q: %w", name, err)
		}
	}

	treeSHA, err := s.runGit(indexEnv, nil, "write-tree")
	if err != nil {
		return fmt.Errorf("evidence: write evidence tree: %w", err)
	}

	commitArgs := []string{"commit-tree", treeSHA, "-m", "evidence: " + runID}
	if hasParent {
		commitArgs = append(commitArgs, "-p", parent)
	}
	commitSHA, err := s.runGit(commitAuthorEnv(), nil, commitArgs...)
	if err != nil {
		return fmt.Errorf("evidence: commit evidence tree: %w", err)
	}

	updateArgs := []string{"update-ref", ref, commitSHA}
	if hasParent {
		updateArgs = append(updateArgs, parent)
	}
	if _, err := s.runGit(nil, nil, updateArgs...); err != nil {
		return fmt.Errorf("evidence: update evidence branch ref: %w", err)
	}
	return nil
}

func (s *OrphanBranchStore) runGit(extraEnv []string, stdin []byte, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = s.RepoPath
	if extraEnv != nil {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func commitAuthorEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=made-evidence",
		"GIT_AUTHOR_EMAIL=made-evidence@localhost",
		"GIT_COMMITTER_NAME=made-evidence",
		"GIT_COMMITTER_EMAIL=made-evidence@localhost",
	}
}
