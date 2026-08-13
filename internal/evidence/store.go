package evidence

const (
	DefaultBranch = "made-evidence"
	DefaultDir    = ".made/evidence"
)

type Config struct {
	StoreInRepo bool
	Dir         string
	Branch      string
}

type Store interface {
	WriteEvidence(runID string, files map[string][]byte) error
}

func NewStore(repoPath string, cfg Config) Store {
	if cfg.StoreInRepo {
		return &InRepoStore{RepoPath: repoPath, Dir: cfg.Dir}
	}
	return &OrphanBranchStore{RepoPath: repoPath, Branch: cfg.Branch}
}
