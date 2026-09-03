package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/daemon"
)

func TestCapabilitiesJSONExposesStructuredRunContract(t *testing.T) {
	var stdout, stderr bytes.Buffer
	stdoutFile := tempOutputFile(t)
	stderrFile := tempOutputFile(t)
	code := run([]string{"capabilities", "--json"}, stdoutFile, stderrFile)
	if code != 0 {
		t.Fatalf("capabilities exit code = %d; stderr=%s", code, readOutputFile(t, stderrFile))
	}
	var payload struct {
		SchemaVersion   int      `json:"schema_version"`
		ProtocolVersion int      `json:"protocol_version"`
		Commands        []string `json:"commands"`
		Agents          []string `json:"agents"`
	}
	if err := json.Unmarshal(readOutputFile(t, stdoutFile), &payload); err != nil {
		t.Fatalf("capabilities output is not JSON: %v", err)
	}
	if payload.SchemaVersion == 0 || payload.ProtocolVersion == 0 {
		t.Fatalf("capabilities versions missing: %+v", payload)
	}
	for _, want := range []string{"run.submit", "run.status", "run.list", "run.cancel", "review.decide", "doctor", "verify", "cursor"} {
		found := false
		for _, got := range payload.Commands {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("capabilities missing command %q: %+v", want, payload.Commands)
		}
	}
	if len(payload.Agents) != 1 || payload.Agents[0] != "codex" {
		t.Fatalf("capabilities agents = %v, want only codex", payload.Agents)
	}
	_ = stdout
	_ = stderr
}

func TestCapabilitiesJSONExposesManagedValidationReviewSourcesAndOptionalStages(t *testing.T) {
	stdoutFile := tempOutputFile(t)
	stderrFile := tempOutputFile(t)
	code := run([]string{"capabilities", "--json"}, stdoutFile, stderrFile)
	if code != 0 {
		t.Fatalf("capabilities exit code = %d; stderr=%s", code, readOutputFile(t, stderrFile))
	}
	var payload struct {
		ManagedValidation struct {
			ReviewSources  []string `json:"review_sources"`
			OptionalStages []string `json:"optional_stages"`
		} `json:"managed_validation"`
	}
	if err := json.Unmarshal(readOutputFile(t, stdoutFile), &payload); err != nil {
		t.Fatalf("capabilities output is not JSON: %v", err)
	}
	for _, want := range []string{"internal", "external"} {
		found := false
		for _, got := range payload.ManagedValidation.ReviewSources {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("managed_validation.review_sources missing %q: %+v", want, payload.ManagedValidation.ReviewSources)
		}
	}
	for _, want := range []string{"review", "test", "document", "lint"} {
		found := false
		for _, got := range payload.ManagedValidation.OptionalStages {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("managed_validation.optional_stages missing %q: %+v", want, payload.ManagedValidation.OptionalStages)
		}
	}
}

func TestObsoleteStatusCommandIsRejected(t *testing.T) {
	stdoutFile := tempOutputFile(t)
	stderrFile := tempOutputFile(t)
	code := run([]string{"status", "--json"}, stdoutFile, stderrFile)
	if code != 2 {
		t.Fatalf("obsolete status exit code = %d, want 2; stderr=%s", code, readOutputFile(t, stderrFile))
	}
}

func TestStatusJSONReportsCurrentStageFromOrderedState(t *testing.T) {
	report := newStatusReport(daemon.RunSnapshot{
		ID:     "run-current-stage",
		Stages: []daemon.StageResult{{Name: "intent", Result: "pass"}, {Name: "review", Result: "pending"}},
	})
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if !strings.Contains(string(data), `"current_stage":"review"`) {
		t.Fatalf("status omitted current stage: %s", data)
	}
}

func tempOutputFile(t *testing.T) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	return file
}

func readOutputFile(t *testing.T, file *os.File) []byte {
	t.Helper()
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatalf("seek output: %v", err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return data
}
