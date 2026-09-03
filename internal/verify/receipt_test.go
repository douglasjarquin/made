package verify_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/verify"
)

func TestReceiptStore_PutGetList(t *testing.T) {
	store := verify.ReceiptStore{Dir: t.TempDir()}
	r := verify.Receipt{
		SchemaVersion: verify.ReceiptSchemaVersion,
		Outcome:       managed.OutcomePassed,
		InputSHA:      "abc123",
		CreatedAt:     time.Now().UTC(),
	}
	if err := store.Put(r); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok, err := store.Get("abc123")
	if err != nil || !ok {
		t.Fatalf("Get: got=%+v ok=%v err=%v", got, ok, err)
	}
	if got.InputSHA != "abc123" {
		t.Errorf("InputSHA = %q, want abc123", got.InputSHA)
	}

	_, ok, err = store.Get("does-not-exist")
	if err != nil {
		t.Fatalf("Get(missing): unexpected error %v", err)
	}
	if ok {
		t.Fatal("Get(missing): expected not found")
	}

	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v, %v", list, err)
	}
}

func TestReceiptStore_RejectsUnsupportedSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sha1.json"), []byte(`{"schema_version":99,"input_sha":"sha1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := verify.ReceiptStore{Dir: dir}
	_, ok, err := store.Get("sha1")
	if err == nil || ok {
		t.Fatalf("expected an error for an unsupported schema_version, got ok=%v err=%v", ok, err)
	}
}

func TestReceiptStore_WritesAreOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	store := verify.ReceiptStore{Dir: dir}
	if err := store.Put(verify.Receipt{SchemaVersion: verify.ReceiptSchemaVersion, InputSHA: "abc"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "abc.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("receipt file mode = %v, want 0600", info.Mode().Perm())
	}
}
