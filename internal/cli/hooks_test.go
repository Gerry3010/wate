package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeHooksIdempotentAndPreserving(t *testing.T) {
	var settings map[string]any
	json.Unmarshal([]byte(`{
	  "model": "opus",
	  "hooks": {
	    "Stop": [{"hooks": [{"type": "command", "command": "notify-send done"}]}],
	    "Notification": [{"matcher": "", "hooks": [{"type": "command", "command": "/old/yate hook Notification"}]}]
	  }
	}`), &settings)
	out := MergeHooks(settings, "/new/yate")
	out = MergeHooks(out, "/new/yate") // second run must not duplicate
	hooks := out["hooks"].(map[string]any)
	if out["model"] != "opus" {
		t.Fatal("unrelated settings lost")
	}
	stop := hooks["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("Stop groups = %d, want 2 (foreign + yate)", len(stop))
	}
	notif := hooks["Notification"].([]any)
	if len(notif) != 1 {
		t.Fatalf("Notification groups = %d, want 1 (old yate replaced)", len(notif))
	}
	b, _ := json.Marshal(notif)
	if !strings.Contains(string(b), `\"/new/yate\" hook Notification`) || strings.Contains(string(b), "/old/yate") {
		t.Fatalf("yate hook not replaced: %s", b)
	}
	for _, ev := range HookEvents {
		if _, ok := hooks[ev]; !ok {
			t.Fatalf("missing hook for %s", ev)
		}
	}
}

func TestInstallHooksCreatesFileAndBackup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".claude", "settings.json")
	if err := InstallHooks(p); err != nil {
		t.Fatal(err)
	}
	if err := InstallHooks(p); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	var backups int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "settings.json.bak-") {
			backups++
		}
	}
	if backups != 1 {
		t.Fatalf("backups = %d, want 1 (only when a file existed)", backups)
	}
	raw, _ := os.ReadFile(p)
	if !json.Valid(raw) {
		t.Fatal("invalid json written")
	}
}
