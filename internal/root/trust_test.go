package root

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrustIsInheritedFromAbove(t *testing.T) {
	f := filepath.Join(t.TempDir(), ".claude.json")
	state := `{"projects":{"/home/u":{"hasTrustDialogAccepted":false},"/home/u/work":{"hasTrustDialogAccepted":true}}}`
	if err := os.WriteFile(f, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	for dir, want := range map[string]bool{
		"/home/u/work":           true,
		"/home/u/work/app/deep/": true,
		"/home/u":                false,
		"/home/u/other":          false,
		"/elsewhere":             false,
	} {
		if got := trustedIn(f, dir); got != want {
			t.Errorf("%s: got %v, want %v", dir, got, want)
		}
	}
	if !trustedIn(filepath.Join(t.TempDir(), "missing.json"), "/x") {
		t.Error("without Claude Code's state there is nothing to go on: say yes")
	}
}
