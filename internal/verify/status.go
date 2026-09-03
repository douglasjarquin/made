package verify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type StatusResult struct {
	Repository string
	InputSHA   string
	Receipt    *Receipt
}

func StatusHead(ctx context.Context, workDir string) (StatusResult, error) {
	root, err := repoRoot(ctx, workDir)
	if err != nil {
		return StatusResult{}, err
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return StatusResult{}, fmt.Errorf("verify: resolve canonical repository root: %w", err)
	}
	head, err := headCommit(ctx, canonicalRoot)
	if err != nil {
		return StatusResult{}, err
	}

	store := ReceiptStore{Dir: ReceiptsDir(StateRoot(canonicalRoot))}
	r, ok, err := store.Get(head)
	if err != nil {
		return StatusResult{}, err
	}
	res := StatusResult{Repository: canonicalRoot, InputSHA: head}
	if ok {
		res.Receipt = &r
	}
	return res, nil
}

func ReceiptForSHA(ctx context.Context, workDir, inputSHA string) (Receipt, bool, error) {
	root, err := repoRoot(ctx, workDir)
	if err != nil {
		return Receipt{}, false, err
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Receipt{}, false, fmt.Errorf("verify: resolve canonical repository root: %w", err)
	}
	store := ReceiptStore{Dir: ReceiptsDir(StateRoot(canonicalRoot))}
	return store.Get(inputSHA)
}

func Clean(ctx context.Context, workDir string) (string, error) {
	root, err := repoRoot(ctx, workDir)
	if err != nil {
		return "", err
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("verify: resolve canonical repository root: %w", err)
	}
	dir := StateRoot(canonicalRoot)
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("verify: clean %q: %w", dir, err)
	}
	return dir, nil
}
