package main

import (
	"os"
	"testing"
)

func TestRunStatusRejectsUnsupportedTrailingArgument(t *testing.T) {
	stdout, stderr := discardOutput(t)
	if code := runExactStatusCommand([]string{"run-1", "unexpected"}, stdout, stderr); code != 2 {
		t.Fatalf("run status exit code = %d, want 2", code)
	}
}

func TestRunCancelRejectsUnsupportedTrailingArgument(t *testing.T) {
	stdout, stderr := discardOutput(t)
	if code := runCancelCommand([]string{"run-1", "unexpected"}, stdout, stderr); code != 2 {
		t.Fatalf("run cancel exit code = %d, want 2", code)
	}
}

func discardOutput(t *testing.T) (stdout, stderr *os.File) {
	t.Helper()
	stdout, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open stdout discard: %v", err)
	}
	stderr, err = os.Open(os.DevNull)
	if err != nil {
		_ = stdout.Close()
		t.Fatalf("open stderr discard: %v", err)
	}
	t.Cleanup(func() {
		_ = stdout.Close()
		_ = stderr.Close()
	})
	return stdout, stderr
}
