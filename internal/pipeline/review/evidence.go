package review

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/douglasjarquin/made/internal/agent"
)

func writeReviewEvidence(ctx context.Context, opts Options, task agent.ReviewTask, response []byte, candidateOutputSHA string) error {
	if opts.Evidence == nil {
		return nil
	}
	if opts.EvidenceRunID == "" {
		return fmt.Errorf("review: evidence run ID is required when evidence storage is configured")
	}
	contract := task.Contract
	contract.CandidateOutputSHA = candidateOutputSHA
	metadata, err := json.Marshal(contract)
	if err != nil {
		return fmt.Errorf("review: encode review evidence metadata: %w", err)
	}
	files := map[string][]byte{
		"review-contract.json": metadata,
		"review-prompt.txt":    []byte(task.Text),
		"review-response.json": append([]byte(nil), response...),
	}
	if contextual, ok := opts.Evidence.(interface {
		WriteEvidenceContext(context.Context, string, map[string][]byte) error
	}); ok {
		if err := contextual.WriteEvidenceContext(ctx, opts.EvidenceRunID, files); err != nil {
			return fmt.Errorf("review: write evidence: %w", err)
		}
		return nil
	}
	if err := opts.Evidence.WriteEvidence(opts.EvidenceRunID, files); err != nil {
		return fmt.Errorf("review: write evidence: %w", err)
	}
	return nil
}
