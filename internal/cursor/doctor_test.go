package cursor_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/cursor"
)

func gitAt(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func newDoctorRepo(t *testing.T, configContent string) string {
	t.Helper()
	dir := t.TempDir()
	gitAt(t, dir, "init", "-b", "main")
	gitAt(t, dir, "config", "user.email", "test@test.local")
	gitAt(t, dir, "config", "user.name", "test")
	if configContent != "" {
		if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte(configContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", "initial")
	return dir
}

func TestDoctor_HealthyWithCursorConfigured(t *testing.T) {
	dir := newDoctorRepo(t, "version: 1\nreview:\n  executors:\n    cursor:\n      model: gpt-5\n")
	if _, err := cursor.Sync(dir, mustConfig(t, dir), false); err != nil {
		t.Fatal(err)
	}
	report := cursor.Doctor(context.Background(), cursor.DoctorParams{Root: dir})
	if !report.Healthy {
		t.Fatalf("expected a healthy report, got %+v", report)
	}
	assertCheck(t, report, "cursor_executor", cursor.StatusOK)
	assertCheck(t, report, "cursor_model", cursor.StatusOK)
	assertCheck(t, report, "projections", cursor.StatusOK)
}

func TestDoctor_UnhealthyWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	gitAt(t, dir, "init", "-b", "main")
	gitAt(t, dir, "config", "user.email", "test@test.local")
	gitAt(t, dir, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", "initial")

	report := cursor.Doctor(context.Background(), cursor.DoctorParams{Root: dir})
	if report.Healthy {
		t.Fatal("expected an unhealthy report with no configuration")
	}
	assertCheck(t, report, "config", cursor.StatusFail)
}

func TestDoctor_ReportsDriftWhenProjectionsStale(t *testing.T) {
	dir := newDoctorRepo(t, "version: 1\nreview:\n  executors:\n    cursor:\n      model: gpt-5\n")
	report := cursor.Doctor(context.Background(), cursor.DoctorParams{Root: dir})
	if report.Healthy {
		t.Fatal("expected an unhealthy report before running made cursor sync")
	}
	assertCheck(t, report, "projections", cursor.StatusFail)
}

func TestDoctor_BaseRefResolvesLocally(t *testing.T) {
	dir := newDoctorRepo(t, "version: 1\n")
	report := cursor.Doctor(context.Background(), cursor.DoctorParams{Root: dir, BaseRef: "HEAD"})
	assertCheck(t, report, "base_ref", cursor.StatusOK)
}

func TestDoctor_BaseRefMissingIsWarnOnlyAndNeverFetches(t *testing.T) {
	dir := newDoctorRepo(t, "version: 1\nreview:\n  executors:\n    cursor:\n      model: gpt-5\n")
	if _, err := cursor.Sync(dir, mustConfig(t, dir), false); err != nil {
		t.Fatal(err)
	}
	report := cursor.Doctor(context.Background(), cursor.DoctorParams{Root: dir, BaseRef: "origin/does-not-exist"})
	if !report.Healthy {
		t.Fatalf("expected an unresolvable --base-ref to stay non-fatal on a shallow/limited clone, got %+v", report)
	}
	assertCheck(t, report, "base_ref", cursor.StatusWarn)
}

func TestDoctor_TempPathsWritable(t *testing.T) {
	dir := newDoctorRepo(t, "version: 1\n")
	report := cursor.Doctor(context.Background(), cursor.DoctorParams{Root: dir})
	assertCheck(t, report, "temp_paths", cursor.StatusOK)
}

func assertCheck(t *testing.T, report cursor.DoctorReport, name string, want cursor.CheckStatus) {
	t.Helper()
	for _, c := range report.Checks {
		if c.Name == name {
			if c.Status != want {
				t.Fatalf("expected check %q to be %q, got %q (%s)", name, want, c.Status, c.Detail)
			}
			return
		}
	}
	t.Fatalf("expected a %q check in report %+v", name, report)
}

func mustConfig(t *testing.T, root string) config.Config {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".made.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "cursor") {
		t.Fatalf("expected fixture config to configure cursor: %s", data)
	}
	cfg, err := config.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
