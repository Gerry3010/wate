package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Gerry3010/wate/internal/tbsock"
)

// tbPane is a pane as terminal-browser names it: its id and the tab it is in.
type tbPane struct {
	ID  string `json:"id"`
	Tab string `json:"tab"`
}

// tbPaneDetails is a pane in a listing. Command stays null: terminal-browser looks up what runs
// on each tty itself.
type tbPaneDetails struct {
	tbPane
	TTY     *string `json:"tty"`
	Command *string `json:"command"`
}

// tbMethods are the calls wate answers; hello says so, and terminal-browser leaves the others
// alone.
var tbMethods = []string{"getCurrentPane", "split", "listPanes", "neighbor", "sendText", "pasteKey", "focusPane"}

// tbOps is what the terminal-browser socket needs from the rest of wate, kept apart so the
// rules can be tested without a window.
type tbOps struct {
	panes func() []PaneInfo
	// ask puts a question to the window that shows pane (see the frontend's answerPaneRequest).
	ask   func(pane, kind string, args map[string]any) (string, error)
	write func(pane, text string) error
	focus func(pane string) error
}

// terminalBrowser answers terminal-browser over its terminal socket, so that `terminal-browser
// open --split` opens its browser in a wate pane beside the one it was run in.
//
// Opening a split and naming panes is what any program in a pane may do through the control
// socket as well. Typing is not: terminal-browser can hand the coding agent an element picked in
// the page, by pasting it into the agent's pane, and that is only let through into the pane a
// browser was opened from, while that browser is open. Any other pane stays the user's.
type terminalBrowser struct {
	ops tbOps

	mu sync.Mutex
	// opener maps each browser pane to the pane it was opened from.
	opener map[string]string
}

func newTerminalBrowser(ops tbOps) *terminalBrowser {
	return &terminalBrowser{ops: ops, opener: map[string]string{}}
}

func (b *terminalBrowser) handle(r tbsock.Request) (any, error) {
	switch r.Method {
	case "hello":
		return map[string]any{"name": "wate", "methods": tbMethods}, nil
	case "getCurrentPane":
		var p struct {
			TTY string `json:"tty"`
		}
		_ = json.Unmarshal(r.Params, &p)
		return map[string]any{"pane": b.paneOnTTY(p.TTY)}, nil
	case "listPanes":
		return map[string]any{"panes": b.list()}, nil
	case "neighbor":
		var p struct {
			From      tbPane `json:"from"`
			Direction string `json:"direction"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, err
		}
		return map[string]any{"pane": b.neighbor(p.From, p.Direction)}, nil
	case "split":
		var p struct {
			From      tbPane   `json:"from"`
			Direction string   `json:"direction"`
			Command   []string `json:"command"`
			Size      *float64 `json:"size"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, err
		}
		return nil, b.split(p.From, p.Direction, p.Command, p.Size)
	case "sendText":
		var p struct {
			Pane string `json:"pane"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, err
		}
		if err := b.mayType(p.Pane); err != nil {
			return nil, err
		}
		// A paste, as terminal-browser means it: the window wraps it in bracketed paste when
		// the program in the pane asked for that, so a line break does not send it off.
		_, err := b.ops.ask(p.Pane, "paste", map[string]any{"text": p.Text})
		return nil, err
	case "pasteKey":
		var p struct {
			Pane string `json:"pane"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, err
		}
		if err := b.mayType(p.Pane); err != nil {
			return nil, err
		}
		// Ctrl+V: Claude Code pastes the image on the clipboard.
		return nil, b.ops.write(p.Pane, "\x16")
	case "focusPane":
		var p struct {
			Pane string `json:"pane"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, err
		}
		if err := b.mayFocus(p.Pane); err != nil {
			return nil, err
		}
		return nil, b.ops.focus(p.Pane)
	}
	return nil, fmt.Errorf("wate does not know %q", r.Method)
}

// paneOnTTY is the pane whose terminal is tty, nil for none (a shell inside tmux or ssh has a
// tty of its own).
func (b *terminalBrowser) paneOnTTY(tty string) *tbPane {
	if tty == "" {
		return nil
	}
	for _, p := range b.ops.panes() {
		if p.TTY == tty {
			return &tbPane{ID: p.PaneID, Tab: p.TabID}
		}
	}
	return nil
}

func (b *terminalBrowser) list() []tbPaneDetails {
	out := []tbPaneDetails{}
	for _, p := range b.ops.panes() {
		d := tbPaneDetails{tbPane: tbPane{ID: p.PaneID, Tab: p.TabID}}
		if p.TTY != "" {
			tty := p.TTY
			d.TTY = &tty
		}
		out = append(out, d)
	}
	return out
}

func (b *terminalBrowser) tabOf(pane string) (string, bool) {
	for _, p := range b.ops.panes() {
		if p.PaneID == pane {
			return p.TabID, true
		}
	}
	return "", false
}

func (b *terminalBrowser) neighbor(from tbPane, direction string) *tbPane {
	if _, _, ok := splitPlacement(direction); !ok {
		return nil
	}
	tab, ok := b.tabOf(from.ID)
	if !ok {
		return nil
	}
	id, err := b.ops.ask(from.ID, "neighbor", map[string]any{"direction": direction})
	if err != nil || id == "" {
		return nil
	}
	return &tbPane{ID: id, Tab: tab}
}

// splitPlacement turns terminal-browser's direction into wate's: "row" puts panes side by side,
// "col" stacks them, and before puts the new one left of or above the old.
func splitPlacement(direction string) (dir string, before bool, ok bool) {
	switch direction {
	case "right":
		return "row", false, true
	case "left":
		return "row", true, true
	case "down":
		return "col", false, true
	case "up":
		return "col", true, true
	}
	return "", false, false
}

func (b *terminalBrowser) split(from tbPane, direction string, command []string, size *float64) error {
	dir, before, ok := splitPlacement(direction)
	if !ok {
		return fmt.Errorf("cannot split towards %q (right, left, down, up)", direction)
	}
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return errors.New("split needs a command to run in the new pane")
	}
	if _, ok := b.tabOf(from.ID); !ok {
		return fmt.Errorf("no pane %s in wate", from.ID)
	}
	args := map[string]any{"dir": dir, "before": before, "command": command, "plain": true}
	if size != nil && *size > 0 && *size < 1 {
		args["ratio"] = *size
	}
	id, err := b.ops.ask(from.ID, "split", args)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.opener[id] = from.ID
	b.mu.Unlock()
	return nil
}

// openers are the panes with a browser open from them right now, and the browsers.
func (b *terminalBrowser) openers() (openers, browsers map[string]bool) {
	live := map[string]bool{}
	for _, p := range b.ops.panes() {
		live[p.PaneID] = true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	openers, browsers = map[string]bool{}, map[string]bool{}
	for browser, opener := range b.opener {
		if !live[browser] {
			delete(b.opener, browser)
			continue
		}
		browsers[browser] = true
		openers[opener] = true
	}
	return openers, browsers
}

// mayType lets text through only into a pane a browser was opened from.
func (b *terminalBrowser) mayType(pane string) error {
	if openers, _ := b.openers(); openers[pane] {
		return nil
	}
	return fmt.Errorf("wate only lets a browser type into the pane it was opened from, and %s is not one", pane)
}

// mayFocus lets the browser bring forward itself or the pane it was opened from.
func (b *terminalBrowser) mayFocus(pane string) error {
	if openers, browsers := b.openers(); openers[pane] || browsers[pane] {
		return nil
	}
	return fmt.Errorf("wate only lets a browser focus itself or the pane it was opened from, and %s is neither", pane)
}

// terminalBrowserOps is the real thing: the panes, their windows and their terminals.
func (c *CtlService) terminalBrowserOps() tbOps {
	return tbOps{
		panes: c.pty.Panes,
		ask:   c.askPane,
		write: c.pty.WriteToPane,
		focus: func(pane string) error {
			if r := c.focusSession(pane); !r.OK {
				return errors.New(r.Error)
			}
			return nil
		},
	}
}
