package verify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/douglasjarquin/made/internal/managed"
)

const (
	RequestSchemaVersion  = 1
	maxRequestBytes       = 1 << 20
	maxTaskFileBytes      = 64 * 1024
	maxBoundedStringBytes = 2000
)

type TaskContext struct {
	Path        string `json:"path,omitempty"`
	ContentHash string `json:"content_hash"`
	Bytes       int    `json:"bytes"`
	Content     string `json:"content"`
}

// Request is the bounded, versioned external-review request `made verify
// prepare` publishes and `made verify complete` strictly re-parses. Its
// Contract field is project issue #39's canonical ReviewContract, unchanged;
// Request only adds the envelope an external caller and `made verify
// complete` need on top of it: run/invocation and repository identity, the
// executor, and optionally a preferred model and bounded task context.
type Request struct {
	SchemaVersion  int                    `json:"schema_version"`
	RunID          string                 `json:"run_id"`
	InvocationID   string                 `json:"invocation_id"`
	Repository     RepoIdentity           `json:"repository"`
	Executor       string                 `json:"executor"`
	RequestedModel string                 `json:"requested_model,omitempty"`
	BaseRef        string                 `json:"base_ref"`
	Config         ConfigIdentity         `json:"config"`
	Contract       managed.ReviewContract `json:"contract"`
	ContractHash   string                 `json:"contract_hash"`
	Task           *TaskContext           `json:"task,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
}

func BuildRequest(rc ResolvedContext, runID, invocationID, executor, requestedModel string, task *TaskContext) (Request, error) {
	if err := boundString("executor", executor, maxBoundedStringBytes); err != nil {
		return Request{}, err
	}
	if err := boundString("requested_model", requestedModel, maxBoundedStringBytes); err != nil {
		return Request{}, err
	}
	contract := managed.BuildReviewContract(rc.BaseSHA, rc.InputSHA, rc.Config.Hash, rc.Guides)
	contractHash, err := contract.Hash()
	if err != nil {
		return Request{}, fmt.Errorf("verify: hash review contract: %w", err)
	}
	return Request{
		SchemaVersion:  RequestSchemaVersion,
		RunID:          runID,
		InvocationID:   invocationID,
		Repository:     rc.Repository,
		Executor:       executor,
		RequestedModel: requestedModel,
		BaseRef:        rc.BaseRef,
		Config:         rc.Config,
		Contract:       contract,
		ContractHash:   contractHash,
		Task:           task,
		CreatedAt:      time.Now().UTC(),
	}, nil
}

func PublishRequest(path string, req Request) error {
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return fmt.Errorf("verify: encode review request: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("verify: create review request directory: %w", err)
	}
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("verify: publish review request: %w", err)
	}
	return nil
}

func LoadRequest(path string) (Request, error) {
	data, err := readBoundedRegularFile(path, maxRequestBytes)
	if err != nil {
		return Request{}, fmt.Errorf("verify: read review request: %w", err)
	}
	var req Request
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return Request{}, fmt.Errorf("verify: parse review request: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Request{}, fmt.Errorf("verify: review request must contain exactly one JSON document")
	}
	if req.SchemaVersion != RequestSchemaVersion {
		return Request{}, fmt.Errorf("verify: review request schema_version %d is not supported (want %d)", req.SchemaVersion, RequestSchemaVersion)
	}
	return req, nil
}

func ReadTaskFile(path string) (*TaskContext, error) {
	if path == "" {
		return nil, nil
	}
	data, err := readBoundedRegularFile(path, maxTaskFileBytes)
	if err != nil {
		return nil, fmt.Errorf("verify: read task file %q: %w", path, err)
	}
	return &TaskContext{
		Path:        path,
		ContentHash: hashBytes(data),
		Bytes:       len(data),
		Content:     string(data),
	}, nil
}
