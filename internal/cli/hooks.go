package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// HookEvents are the Claude Code hook events wate listens to.
var HookEvents = []string{"SessionStart", "UserPromptSubmit", "Notification", "Stop", "SessionEnd"}

// wateHookRe recognises our own hook commands regardless of binary path or quoting.
// Also matches hooks installed under the project's old name (yate) so they get replaced.
var wateHookRe = regexp.MustCompile(`[wy]ate"? hook \w+`)

// InstallHooks merges wate's hook commands into a Claude Code settings.json, idempotently,
// keeping every other hook and setting intact. A backup is written next to the file.
func InstallHooks(path string) error {
	settings := map[string]any{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &settings); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		backup := path + ".bak-" + time.Now().Format("20060102-150405")
		if err := os.WriteFile(backup, raw, 0o600); err != nil {
			return err
		}
	case os.IsNotExist(err):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
	default:
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "wate"
	}
	updated := MergeHooks(settings, exe)
	out, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

// MergeHooks returns settings with a `wate hook <event>` command registered for each event.
// Existing wate entries are replaced (so the binary path can change); other hooks are untouched.
func MergeHooks(settings map[string]any, exe string) map[string]any {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, ev := range HookEvents {
		var groups []any
		if existing, ok := hooks[ev].([]any); ok {
			for _, g := range existing {
				if !isWateGroup(g) {
					groups = append(groups, g)
				}
			}
		}
		groups = append(groups, map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": fmt.Sprintf("%q hook %s", exe, ev),
				"timeout": 5,
			}},
		})
		hooks[ev] = groups
	}
	settings["hooks"] = hooks
	return settings
}

func isWateGroup(g any) bool {
	group, ok := g.(map[string]any)
	if !ok {
		return false
	}
	list, _ := group["hooks"].([]any)
	for _, h := range list {
		hm, _ := h.(map[string]any)
		if cmd, _ := hm["command"].(string); wateHookRe.MatchString(cmd) {
			return true
		}
	}
	return false
}
