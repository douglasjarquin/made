package verify

import (
	"context"
	"fmt"

	"github.com/douglasjarquin/made/internal/managed"
)

type RunParams struct {
	WorkDir string
	BaseRef string
}

type RunOutcome struct {
	Context ResolvedContext
	Engine  EngineResult
	Receipt Receipt
}

func Run(ctx context.Context, p RunParams) (RunOutcome, error) {
	rc, err := ResolveContext(ctx, p.WorkDir, p.BaseRef)
	if err != nil {
		return RunOutcome{}, err
	}

	runID := "verify-" + rc.InputSHA[:12]
	stateDir := StateRoot(rc.Repository.Root)

	res, err := RunEngine(ctx, EngineParams{
		Workspace:    rc.Repository.Root,
		BaseSHA:      rc.BaseSHA,
		InputSHA:     rc.InputSHA,
		ConfigPath:   rc.Config.Path,
		PolicyHash:   rc.Config.Hash,
		ReviewSource: managed.ReviewSourceInternal,
		RunID:        runID,
		MissionID:    "made-verify",
		StateDir:     stateDir,
	})
	if err != nil {
		return RunOutcome{}, err
	}

	contract := managed.BuildReviewContract(rc.BaseSHA, rc.InputSHA, rc.Config.Hash, rc.Guides)
	contractHash, _ := contract.Hash()
	review := reviewReceiptFromResult(res, managed.ReviewSourceInternal, "made", "", "", "", contractHash)
	receipt := BuildReceipt(rc.Repository.Root, rc.BaseSHA, rc.InputSHA, rc.Config, review, res)

	store := ReceiptStore{Dir: ReceiptsDir(stateDir)}
	if err := store.Put(receipt); err != nil {
		return RunOutcome{}, fmt.Errorf("verify: write receipt: %w", err)
	}

	return RunOutcome{Context: rc, Engine: res, Receipt: receipt}, nil
}
