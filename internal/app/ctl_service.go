package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Gerry3010/wate/internal/config"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/wate/internal/ctl"
	"github.com/Gerry3010/wate/internal/theme"
)

// OpenRequest is emitted to the frontend as "ctl:open".
type OpenRequest struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
	Pane string `json:"pane"`
	Tab  string `json:"tab"`
}

// ActionRequest is emitted as "ctl:action".
type ActionRequest struct {
	Name string `json:"name"`
	Pane string `json:"pane"`
	Tab  string `json:"tab"`
}

// HookEvent is emitted as "agent:hook" (Claude Code hooks).
type HookEvent struct {
	Event string          `json:"event"`
	Pane  string          `json:"pane"`
	Tab   string          `json:"tab"`
	Data  json.RawMessage `json:"data"`
}

// CtlService owns the control socket and exposes its path to the frontend/shell env.
type CtlService struct {
	pty     *PtyService
	cfg     *ConfigService
	windows *WindowService
	bridge  *PaneBridge
	access  *AccessService
	// Restart is set after construction: the restart service needs the agent service, which
	// needs this one.
	Restart *RestartService
	server  *ctl.Server
	// OnHook is set by the agent service.
	OnHook func(HookEvent)
}

func NewCtlService(pty *PtyService, cfg *ConfigService, windows *WindowService, bridge *PaneBridge, access *AccessService) *CtlService {
	return &CtlService{pty: pty, cfg: cfg, windows: windows, bridge: bridge, access: access}
}

// target picks the window a request is about, from the pane or tab it names.
func (c *CtlService) target(pane, tab string) *application.WebviewWindow {
	if c.windows == nil {
		return nil
	}
	w, _ := c.windows.windowFor(pane, tab)
	return w
}

func (c *CtlService) ServiceName() string { return "CtlService" }

func (c *CtlService) ServiceStartup(context.Context, application.ServiceOptions) error {
	s, err := ctl.Listen(ctl.SocketPath(os.Getpid()), c.handle)
	if err != nil {
		return err
	}
	c.server = s
	c.pty.SetSocketPath(s.Path())
	// Record the socket for later starts with the same config (see ctl.FindPrimary). A secondary
	// window must not take that role over from the instance that owns the session.
	if !c.cfg.Secondary {
		if err := os.MkdirAll(config.StateDir(), 0o755); err == nil {
			_ = os.WriteFile(ctl.PrimaryFile(config.StateDir()), []byte(s.Path()+"\n"), 0o600)
		}
	}
	return nil
}

func (c *CtlService) ServiceShutdown() error {
	c.Resign()
	if c.server != nil {
		return c.server.Close()
	}
	return nil
}

// Resign drops the primary-instance record (if it is ours). Called as soon as shutdown begins:
// the process stays alive for a few seconds while Claude sessions and shells wind down, and a
// wate started in that window must take over and restore the session rather than come up as
// an empty secondary window.
func (c *CtlService) Resign() {
	if c.server == nil {
		return
	}
	path := ctl.PrimaryFile(config.StateDir())
	if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) == c.server.Path() {
		_ = os.Remove(path)
	}
}

// SocketPath is what shells get as $WATE_SOCKET.
func (c *CtlService) SocketPath() string {
	if c.server == nil {
		return ""
	}
	return c.server.Path()
}

func (c *CtlService) handle(r ctl.Request) ctl.Response {
	app := application.Get()
	switch r.Cmd {
	case "ping":
		return ctl.Response{OK: true, Data: "pong"}
	case "open":
		p := r.Path
		if !filepath.IsAbs(p) {
			cwd, _ := os.Getwd()
			p = filepath.Join(cwd, p)
		}
		if _, err := os.Stat(p); err != nil {
			return ctl.Response{Error: err.Error()}
		}
		emitTo(c.target(r.Pane, r.Tab), "ctl:open", OpenRequest{Path: p, Line: r.Line, Col: r.Col, Pane: r.Pane, Tab: r.Tab})
		return ctl.Response{OK: true}
	case "new-window":
		cwd := r.Path
		if cwd != "" {
			if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
				return ctl.Response{Error: "not a directory: " + cwd}
			}
		}
		if c.windows == nil {
			return ctl.Response{Error: "no window service"}
		}
		id, err := c.windows.NewWindow(context.Background(), cwd)
		if err != nil {
			return ctl.Response{Error: err.Error()}
		}
		return ctl.Response{OK: true, Data: id}
	case "new-tab":
		p := r.Path
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			return ctl.Response{Error: "not a directory: " + p}
		}
		w := c.target(r.Pane, r.Tab)
		emitTo(w, "ctl:new-tab", OpenRequest{Path: p})
		if w != nil {
			application.InvokeSync(func() {
				if w.IsMinimised() {
					w.UnMinimise()
				}
				w.Focus()
			})
		}
		return ctl.Response{OK: true}
	case "input":
		// Writes straight into the PTY of the pane (by WATE_PANE_ID) — no frontend round trip.
		if err := c.pty.WriteToPane(r.Pane, r.Text); err != nil {
			return ctl.Response{Error: err.Error()}
		}
		return ctl.Response{OK: true}
	case "restart":
		if c.Restart == nil {
			return ctl.Response{Error: "restarts are not set up"}
		}
		return ctl.Response{OK: true, Data: c.Restart.Announce(r.Text, r.Lines, true)}
	case "restart-status":
		if c.Restart == nil {
			return ctl.Response{Error: "restarts are not set up"}
		}
		return ctl.Response{OK: true, Data: c.Restart.Status()}
	case "restart-now":
		// Deliberately not an MCP tool: ending the user's terminal is the user's decision, and
		// an agent that wanted to do it anyway could already run pkill. What this is for is
		// scripting and the dialog's own button.
		if c.Restart == nil {
			return ctl.Response{Error: "restarts are not set up"}
		}
		go c.Restart.Proceed(r.Name != "quit")
		return ctl.Response{OK: true}
	case "restart-respond":
		if c.Restart == nil {
			return ctl.Response{Error: "restarts are not set up"}
		}
		if err := c.Restart.Respond(c.caller(r), r.Name, r.Lines, r.Text); err != nil {
			return ctl.Response{Error: err.Error()}
		}
		return ctl.Response{OK: true}
	case "pane-read", "pane-split", "pane-close", "pane-focus", "pane-list", "pane-status", "split-ratio", "pane-write", "pane-move":
		data, err := c.pane(r)
		if err != nil {
			return ctl.Response{Error: err.Error()}
		}
		return ctl.Response{OK: true, Data: data}
	case "debug":
		// Deliberately a broadcast: `wate ctl debug` should dump every window, not just one.
		app.Event.Emit("ctl:action", ActionRequest{Name: "__debug"})
		return ctl.Response{OK: true}
	case "action":
		if strings.TrimSpace(r.Name) == "" {
			return ctl.Response{Error: "action name required"}
		}
		emitTo(c.target(r.Pane, r.Tab), "ctl:action", ActionRequest{Name: r.Name, Pane: r.Pane, Tab: r.Tab})
		return ctl.Response{OK: true}
	case "import-theme":
		id, err := theme.ImportFile(r.Path, userThemesDir())
		if err != nil {
			return ctl.Response{Error: err.Error()}
		}
		if _, err := c.cfg.Set(map[string]any{"general.theme": id}); err != nil {
			return ctl.Response{Error: err.Error()}
		}
		return ctl.Response{OK: true, Data: id}
	case "hook":
		ev := HookEvent{Event: r.Event, Pane: r.Pane, Tab: r.Tab, Data: r.Data}
		// OnHook is always wired (main.go); there is no frontend listener for this.
		if c.OnHook != nil {
			c.OnHook(ev)
		}
		// The hook is wate's only way into a running session that does not type into its
		// terminal, so anything wate needs to tell the agent rides back on this reply and the
		// hook prints it as context. Usually there is nothing to say.
		if c.Restart != nil {
			if text := c.Restart.TellPane(r.Pane); text != "" {
				return ctl.Response{OK: true, Data: text}
			}
		}
		return ctl.Response{OK: true}
	default:
		slog.Debug("ctl: unknown command", "cmd", r.Cmd)
		return ctl.Response{Error: "unknown command " + r.Cmd}
	}
}

var errNoPane = errors.New("no such pane")

// caller is the pane a request came from: its token if it sent one, else what it claims.
//
// The token is minted per pane and handed to the shell, so a program in a pane proves where it
// is sitting rather than being taken at its word. Falling back to the claim keeps `wate ctl`
// usable from a script that only has a pane id — the point of the token is not to lock the
// socket down (it is 0600 on one user's machine) but to make naming somebody else's pane a
// deliberate act instead of the default.
func (c *CtlService) caller(r ctl.Request) string {
	if pane, ok := c.pty.PaneForToken(r.Token); ok {
		return pane
	}
	return r.Pane
}

// targetOf is the pane a request acts on: the one it names, else the caller's own.
//
// Who is asking and what is being acted on are two different things, and they have to stay
// that way — fold them together and every rights check passes, because the caller is always
// allowed to act on itself.
func targetOf(r ctl.Request, caller string) string {
	if r.Target != "" {
		return r.Target
	}
	return caller
}

// pane handles everything that acts on one pane, with the rights checked first.
func (c *CtlService) pane(r ctl.Request) (any, error) {
	from := c.caller(r)
	if from == "" {
		return nil, errors.New("no pane: run this from inside a wate pane, or pass --pane")
	}
	if _, ok := c.pty.SessionForPane(from); !ok {
		return nil, fmt.Errorf("no such pane %q", from)
	}
	target := targetOf(r, from)

	switch r.Cmd {
	case "pane-split":
		dir := r.Dir
		if dir == "" {
			dir = "row"
		}
		if dir != "row" && dir != "col" {
			return nil, fmt.Errorf("dir must be row or col, not %q", dir)
		}
		id, err := c.askPane(from, "split", map[string]any{"dir": dir, "ratio": r.Ratio})
		if err != nil {
			return nil, err
		}
		// Whoever asked for the pane owns it, and may read and write it without being granted
		// anything: it is the agent's own workspace, not the user's.
		c.access.Opened(from, id)
		return id, nil

	case "pane-list":
		return c.askPane(from, "list", map[string]any{})

	case "pane-status":
		// Answered from the backend, not the window: this is what a poll hits over and over
		// while it waits for a command to finish, and it has to be cheap and current rather
		// than a round trip through a web view that is busy drawing the output.
		if _, ok := c.pty.TabForPane(target); !ok {
			return nil, fmt.Errorf("no such pane %q", target)
		}
		if ct, _ := c.pty.TabForPane(from); ct != "" {
			if tt, _ := c.pty.TabForPane(target); tt != ct {
				return nil, fmt.Errorf("pane %q is in another tab", target)
			}
		}
		cmd, err := c.pty.Command(target)
		if err != nil {
			return nil, err
		}
		return cmd, nil

	case "split-ratio":
		if err := c.access.Allow(from, target, RightManage); err != nil {
			return nil, err
		}
		dir := r.Dir
		if dir == "" {
			dir = "row"
		}
		return c.askPane(target, "ratio", map[string]any{"dir": dir, "ratio": r.Ratio})

	case "pane-close":
		if err := c.access.Allow(from, target, RightManage); err != nil {
			return nil, err
		}
		return c.askPane(target, "close", map[string]any{})

	case "pane-focus":
		if err := c.access.Allow(from, target, RightManage); err != nil {
			return nil, err
		}
		return c.askPane(target, "focus", map[string]any{})

	case "pane-read":
		if err := c.access.Allow(from, target, RightRead); err != nil {
			return nil, err
		}
		return c.askPane(target, "read", map[string]any{"lines": r.Lines})

	case "pane-move":
		// Only a pane the caller opened may be moved, so a user's pane cannot be carried off.
		// Ownership survives the move, but reach does not: while the pane sits in another tab
		// its owner cannot read or write it, because the tab is the boundary. Moving it back
		// is still allowed, and restores the rest.
		if !c.access.Owns(from, target) {
			return nil, fmt.Errorf("pane %q is not yours to move", target)
		}
		return c.askPane(target, "move", map[string]any{"to": r.Name})

	case "pane-write":
		if err := c.access.Allow(from, target, RightWrite); err != nil {
			return nil, err
		}
		text := r.Text
		if !c.access.Owns(from, target) {
			// Somebody else's pane: put the text on the prompt, let the user press Enter.
			text = typable(text)
		}
		if err := c.pty.WriteToPane(target, text); err != nil {
			return nil, err
		}
		return "", nil
	}
	return nil, fmt.Errorf("unknown command %q", r.Cmd)
}

// askPane puts a question to the window that owns `pane` and returns its answer.
func (c *CtlService) askPane(pane, kind string, args map[string]any) (string, error) {
	if c.bridge == nil {
		return "", errors.New("no pane bridge")
	}
	if _, ok := c.pty.SessionForPane(pane); !ok {
		return "", fmt.Errorf("no such pane %q", pane)
	}
	payload, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	return c.bridge.ask(c.target(pane, ""), kind, pane, string(payload))
}
