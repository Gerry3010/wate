package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gerry3010/wate/internal/config"
	"github.com/Gerry3010/wate/internal/ctl"
)

// The restart manifest: what was running when wate restarted, written just before it quits.
//
// wate brings the tabs back by itself (session.json) and offers each Claude session for
// resuming in its old pane. The manifest is what lets the root agent check that afterwards and
// put right what did not come back: a session nobody resumed, a tab colour, Remote Control.
// Pane and tab ids are new after a restart, so it names things the way they survive one: tabs
// by position and title, sessions by their Claude Code session id.

// manifestFresh is how long after a restart the root agent's first prompt points at the
// manifest. A root agent that starts later than that is not coming back from this restart.
const manifestFresh = 15 * time.Minute

// RestartManifest is restart-manifest.json.
type RestartManifest struct {
	WrittenAt string `json:"written_at"`
	// RequestedBy is "root" or "user"; Reason is what was given.
	RequestedBy string           `json:"requested_by"`
	Reason      string           `json:"reason,omitempty"`
	Windows     []ManifestWindow `json:"windows"`
}

type ManifestWindow struct {
	Index int           `json:"index"`
	Tabs  []ManifestTab `json:"tabs"`
}

type ManifestTab struct {
	// Index is the tab's place in the bar, from 1.
	Index  int            `json:"index"`
	Title  string         `json:"title"`
	Color  string         `json:"color,omitempty"`
	Active bool           `json:"active,omitempty"`
	Panes  []ManifestPane `json:"panes"`
}

type ManifestPane struct {
	Kind    string `json:"kind"`
	Cwd     string `json:"cwd,omitempty"`
	Command string `json:"command,omitempty"`
	// The Claude Code session running in it, if any.
	SessionID     string `json:"session_id,omitempty"`
	SessionTitle  string `json:"session_title,omitempty"`
	OpenedByRoot  bool   `json:"opened_by_root,omitempty"`
	RemoteControl bool   `json:"remote_control,omitempty"`
}

func manifestPath() string { return filepath.Join(config.StateDir(), "restart-manifest.json") }

// PrepareRelaunch wires what a restart does just before it quits: check that the installed
// wate runs, then write the manifest. A package function rather than a method: every exported
// method of a bound service becomes a frontend binding, and this one is wiring.
func PrepareRelaunch(s *RestartService, c *CtlService) {
	if s == nil || c == nil {
		return
	}
	s.beforeRelaunch = func(rec RestartRecord) error {
		if err := c.checkRelaunch(); err != nil {
			return err
		}
		if err := c.writeManifest(rec); err != nil {
			// Not worth calling a restart off for: wate restores the tabs without it.
			slog.Warn("restart: could not write the manifest", "err", err)
		}
		return nil
	}
}

// checkRelaunch makes sure the wate that a restart starts runs at all: it is the binary on
// disk now, which may have been replaced since this one started. It is asked to ping this
// instance, which every version of it can.
func (c *CtlService) checkRelaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot tell which wate would start: %w", err)
	}
	if _, err := os.Stat(exe); err != nil {
		return fmt.Errorf("%s is missing", exe)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "ctl", "ping")
	if c.server != nil {
		cmd.Env = append(os.Environ(), "WATE_SOCKET="+c.server.Path())
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("the installed wate (%s) does not run: %v %s", exe, err, firstLine(string(out), 200))
	}
	return nil
}

// writeManifest records every window's tabs and what runs in their panes.
func (c *CtlService) writeManifest(rec RestartRecord) error {
	m := RestartManifest{WrittenAt: time.Now().Format(time.RFC3339), RequestedBy: "user", Reason: rec.Reason}
	if rec.By == "root" {
		m.RequestedBy = "root"
	}
	sessions := map[string]RootSession{}
	if c.Root != nil {
		for _, s := range c.Root.Sessions() {
			sessions[s.PaneID] = s
		}
	}
	if c.windows != nil {
		for _, info := range c.windows.Windows() {
			win, ok := c.windows.byID(WindowID(info.ID))
			if !ok {
				continue
			}
			reply, err := c.bridge.ask(win, "tabs", "", "")
			if err != nil {
				slog.Warn("restart manifest: a window did not list its tabs", "window", info.ID, "err", err)
				continue
			}
			mw, err := manifestWindow(info.Index, reply, sessions)
			if err != nil {
				return err
			}
			m.Windows = append(m.Windows, mw)
		}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	p := manifestPath()
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// manifestWindow turns a window's tab list (the frontend's "tabs" answer) into the manifest's
// form, with the session facts the backend knows per pane.
func manifestWindow(index int, reply string, sessions map[string]RootSession) (ManifestWindow, error) {
	var tabs []struct {
		Title  string `json:"title"`
		Color  string `json:"color"`
		Index  int    `json:"index"`
		Active bool   `json:"active"`
		Panes  []struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			Cwd     string `json:"cwd"`
			Command string `json:"command"`
		} `json:"panes"`
	}
	if err := json.Unmarshal([]byte(reply), &tabs); err != nil {
		return ManifestWindow{}, fmt.Errorf("a window's tab list: %w", err)
	}
	mw := ManifestWindow{Index: index}
	for _, t := range tabs {
		mt := ManifestTab{Index: t.Index, Title: t.Title, Color: t.Color, Active: t.Active}
		for _, p := range t.Panes {
			mp := ManifestPane{Kind: p.Kind, Cwd: p.Cwd, Command: p.Command}
			if s, ok := sessions[p.ID]; ok {
				// A session without a title of its own yet is named by its last prompt.
				mp.SessionID, mp.SessionTitle = s.SessionID, firstNonEmpty(s.Title, s.Message)
				mp.OpenedByRoot, mp.RemoteControl = s.OpenedByRoot, s.RemoteControl
				if mp.Cwd == "" {
					mp.Cwd = s.Cwd
				}
			}
			mt.Panes = append(mt.Panes, mp)
		}
		mw.Tabs = append(mw.Tabs, mt)
	}
	return mw, nil
}

// readManifest is the last manifest written, if any.
func readManifest() (RestartManifest, []byte, bool) {
	data, err := os.ReadFile(manifestPath())
	if err != nil {
		return RestartManifest{}, nil, false
	}
	var m RestartManifest
	if json.Unmarshal(data, &m) != nil {
		return RestartManifest{}, nil, false
	}
	return m, data, true
}

// freshManifest is the manifest of a restart that has just happened, for the root agent's
// first prompt; ok is false when there is none that recent.
func freshManifest(now time.Time) (RestartManifest, bool) {
	m, _, ok := readManifest()
	if !ok {
		return m, false
	}
	at, err := time.Parse(time.RFC3339, m.WrittenAt)
	if err != nil || now.Sub(at) > manifestFresh || now.Before(at.Add(-time.Minute)) {
		return m, false
	}
	return m, true
}

// restartRoot handles the root agent's restart verbs. It may start a round and call off its own,
// never force one: that stays the user's.
func (c *CtlService) restartRoot(r ctl.Request) ctl.Response {
	if c.Restart == nil {
		return ctl.Response{Error: "restarts are not set up"}
	}
	switch r.Cmd {
	case "restart-root":
		minutes := r.Lines
		if minutes <= 0 && c.cfg != nil {
			minutes = c.cfg.Current().Claude.RestartDeadline
		}
		st, err := c.Restart.announceForRoot(strings.TrimSpace(r.Text), minutes)
		if err != nil {
			return ctl.Response{Error: err.Error()}
		}
		return ctl.Response{OK: true, Data: st}
	case "restart-cancel":
		st, err := c.Restart.cancelForRoot()
		if err != nil {
			return ctl.Response{Error: err.Error()}
		}
		return ctl.Response{OK: true, Data: st}
	case "restart-manifest":
		_, data, ok := readManifest()
		if !ok {
			return ctl.Response{Error: "no restart manifest: wate writes one just before a restart"}
		}
		return ctl.Response{OK: true, Data: json.RawMessage(data)}
	}
	return ctl.Response{Error: "unknown command " + r.Cmd}
}

// resumeSessions brings back the Claude sessions a restored pane offers, the same as its
// Resume block: one by session id, or every one with an empty id. Each window is asked.
func (c *CtlService) resumeSessions(session string) ctl.Response {
	if c.windows == nil {
		return ctl.Response{Error: "no windows"}
	}
	session = strings.TrimSpace(session)
	data, _ := json.Marshal(map[string]string{"session": session})
	resumed := []json.RawMessage{}
	for _, info := range c.windows.Windows() {
		win, ok := c.windows.byID(WindowID(info.ID))
		if !ok {
			continue
		}
		reply, err := c.bridge.ask(win, "resume", "", string(data))
		if err != nil {
			slog.Debug("resume: a window did not answer", "window", info.ID, "err", err)
			continue
		}
		var got []json.RawMessage
		if json.Unmarshal([]byte(reply), &got) == nil {
			resumed = append(resumed, got...)
		}
	}
	if session != "" && len(resumed) == 0 {
		return ctl.Response{Error: errors.New("no pane offers session " + session +
			" for resuming; it may be running already (see wate_sessions)").Error()}
	}
	return ctl.Response{OK: true, Data: resumed}
}
