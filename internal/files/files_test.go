package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadWriteRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.md")
	os.WriteFile(p, []byte("# hi\n"), 0o600)
	d, err := Read(p)
	if err != nil || d.Content != "# hi\n" || d.Name != "a.md" {
		t.Fatalf("read: %v %+v", err, d)
	}
	d2, err := Write(p, "# changed\n", d.Hash)
	if err != nil || d2.Hash == d.Hash {
		t.Fatalf("write: %v", err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("permissions not preserved: %v", st.Mode())
	}
	// Someone else edits the file → stale hash must be refused.
	os.WriteFile(p, []byte("external\n"), 0o600)
	if _, err := Write(p, "mine\n", d2.Hash); !errors.Is(err, ErrChanged) {
		t.Fatalf("want ErrChanged, got %v", err)
	}
	if _, err := Write(p, "forced\n", ""); err != nil {
		t.Fatal(err)
	}
}

func TestReadRejects(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(dir); err == nil {
		t.Fatal("directory must be rejected")
	}
	bin := filepath.Join(dir, "b")
	os.WriteFile(bin, []byte{0xff, 0xfe, 0x00}, 0o644)
	if _, err := Read(bin); err == nil {
		t.Fatal("invalid utf-8 must be rejected")
	}
	if _, err := Read(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("missing file must error")
	}
}
