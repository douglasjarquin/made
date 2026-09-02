package receipt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/evidence"
	execpkg "github.com/douglasjarquin/made/internal/exec"
)

// ReceiptSchemaVersion is bumped whenever Receipt's fields change in a way
// Get must reject rather than misinterpret.
const ReceiptSchemaVersion = 1

// DefaultBranch is the orphan git branch receipts publish to when Store.Branch
// is unset, distinct from evidence.DefaultBranch so receipts and run evidence
// never collide.
const DefaultBranch = "made-receipts"

// Receipt records one successful validation-lane run well enough to decide,
// later, whether it can stand in for running the same work again. Only
// successful results are ever constructed - a failure is never published,
// so "no receipt found" and "the last attempt failed" are indistinguishable
// by design (see issue #33 Phase 3: "do not reuse failures by default").
type Receipt struct {
	SchemaVersion int         `json:"schema_version"`
	Fingerprint   Fingerprint `json:"fingerprint"`
	SourceRunID   string      `json:"source_run_id"`
	StartedAt     time.Time   `json:"started_at"`
	CompletedAt   time.Time   `json:"completed_at"`
	MadeVersion   string      `json:"made_version"`
}

// Store durably publishes and looks up Receipts, content-addressed by their
// Fingerprint hash, on a dedicated orphan git branch - reusing
// evidence.OrphanBranchStore's already-tested atomic write/publish
// machinery for Put rather than a second implementation.
type Store struct {
	RepoPath string
	Branch   string
	// MaxAge, when non-zero, is a logical retention window: Get treats any
	// receipt whose CompletedAt is older than MaxAge as not found, even
	// though it remains durably stored. This never rewrites or deletes git
	// history - true object-level garbage collection needs branch history
	// rewriting, which conflicts with every other operation on this branch
	// being fast-forward-only and non-destructive, and is deliberately not
	// implemented here. Zero means no age-based expiry.
	MaxAge time.Duration
}

func (s *Store) branch() string {
	if s.Branch == "" {
		return DefaultBranch
	}
	return s.Branch
}

// Put durably and atomically publishes r under fingerprintHash and pushes
// it to origin, returning the commit SHA it landed on.
func (s *Store) Put(ctx context.Context, fingerprintHash string, r Receipt) (string, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("receipt: encode receipt for %s: %w", fingerprintHash, err)
	}
	store := &evidence.OrphanBranchStore{RepoPath: s.RepoPath, Branch: s.branch()}
	runID := receiptPath(fingerprintHash)
	if err := store.WriteEvidenceContext(ctx, runID, map[string][]byte{"receipt.json": data}); err != nil {
		return "", fmt.Errorf("receipt: write %s: %w", fingerprintHash, err)
	}
	sha, err := store.PublishEvidenceSHA(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("receipt: publish %s: %w", fingerprintHash, err)
	}
	return sha, nil
}

// Get looks up a previously published receipt by fingerprint hash. It never
// returns a hard error: any problem at all - the receipts branch not
// existing yet, this fingerprint never having been published, corrupt JSON,
// an unsupported schema version, a git failure - fails open, returning
// ok=false and a human-readable reason. Per issue #33's explicit policy, a
// receipt-store problem must always fall back to real execution, never
// block or fail a run.
//
// Get only reads local git refs; it never fetches from origin. Within a
// single made gate, every worktree shares one bare repository's refs, so a
// receipt Put by any run there is immediately visible without a fetch. A
// separate, freshly cloned gate would not see it - an accepted limitation.
func (s *Store) Get(ctx context.Context, fingerprintHash string) (Receipt, bool, string) {
	ref := "refs/heads/" + s.branch() + ":" + receiptPath(fingerprintHash) + "/receipt.json"
	res, err := execpkg.Run(ctx, execpkg.Command{Name: "git", Args: []string{"show", ref}, Dir: s.RepoPath})
	if err != nil || res.ExitCode != 0 {
		return Receipt{}, false, "no receipt found for " + fingerprintHash
	}
	var r Receipt
	if err := json.Unmarshal(res.Stdout, &r); err != nil {
		return Receipt{}, false, "corrupt receipt JSON: " + err.Error()
	}
	if r.SchemaVersion != ReceiptSchemaVersion {
		return Receipt{}, false, fmt.Sprintf("unsupported receipt schema version %d", r.SchemaVersion)
	}
	if s.MaxAge > 0 {
		if age := time.Since(r.CompletedAt); age > s.MaxAge {
			return Receipt{}, false, fmt.Sprintf("receipt expired: completed %s ago, max age %s", age.Round(time.Second), s.MaxAge)
		}
	}
	return r, true, "found"
}

func receiptPath(fingerprintHash string) string {
	return "receipts/" + strings.ReplaceAll(fingerprintHash, ":", "-")
}

// List returns every receipt published on this branch - diagnostics only,
// ignoring MaxAge entirely (a caller inspecting what's stored wants to see
// expired entries too, not have them silently hidden). Like Get, List never
// hard-errors on a state that just means "nothing to list": a nonexistent
// branch or corrupt individual entries are skipped rather than failing the
// whole call, since diagnostics must remain usable even when some receipts
// are unreadable.
func (s *Store) List(ctx context.Context) ([]Receipt, error) {
	branch := s.branch()
	res, err := execpkg.Run(ctx, execpkg.Command{
		Name: "git",
		Args: []string{"ls-tree", "-r", "--name-only", "refs/heads/" + branch},
		Dir:  s.RepoPath,
	})
	if err != nil || res.ExitCode != 0 {
		return nil, nil
	}

	var receipts []Receipt
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		path := strings.TrimSpace(line)
		if path == "" || !strings.HasSuffix(path, "/receipt.json") {
			continue
		}
		show, err := execpkg.Run(ctx, execpkg.Command{
			Name: "git",
			Args: []string{"show", "refs/heads/" + branch + ":" + path},
			Dir:  s.RepoPath,
		})
		if err != nil || show.ExitCode != 0 {
			continue
		}
		var r Receipt
		if err := json.Unmarshal(show.Stdout, &r); err != nil || r.SchemaVersion != ReceiptSchemaVersion {
			continue
		}
		receipts = append(receipts, r)
	}
	return receipts, nil
}
