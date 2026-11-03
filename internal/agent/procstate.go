package agent

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

// ProcState is what Claude Code keeps about itself in ~/.claude/sessions/<pid>.json while it
// runs. The file is Claude Code's own and undocumented; wate reads it only for what no hook
// tells: whether the prompt is waiting this moment, and whether Remote Control is on.
type ProcState struct {
	SessionID string `json:"sessionId"`
	// Status is "idle" at the prompt, "shell" at the prompt with a background shell running,
	// "busy" during a turn and "waiting" while a dialog or permission prompt is open, which
	// WaitingFor names.
	Status     string `json:"status"`
	WaitingFor string `json:"waitingFor"`
	// BridgeSessionID is set while Remote Control is on and cleared when it is disconnected.
	BridgeSessionID string `json:"bridgeSessionId"`
}

// ReadProcState reads the state of the Claude Code process pid.
func ReadProcState(home string, pid int) (ProcState, error) {
	var s ProcState
	b, err := os.ReadFile(filepath.Join(home, ".claude", "sessions", strconv.Itoa(pid)+".json"))
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(b, &s)
	return s, err
}

// AtPrompt reports whether the session sits at its prompt, with no turn running and no dialog
// open.
func (s ProcState) AtPrompt() bool {
	return s.Status == "idle" || s.Status == "shell"
}

// RemoteControlURL is where the session can be continued while Remote Control is on, "" when
// it is off.
func (s ProcState) RemoteControlURL() string {
	if s.BridgeSessionID == "" {
		return ""
	}
	return "https://claude.ai/code/" + url.PathEscape(s.BridgeSessionID)
}
