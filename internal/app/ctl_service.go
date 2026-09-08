package app

import (
	"context"
	"encoding/json"
	"errors"
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
	pty    *PtyService
	cfg    *ConfigService
	server *ctl.Server
	// OnHook is set by the agent service.
	OnHook func(HookEvent)
}

func NewCtlService(pty *PtyService, cfg *ConfigService) *CtlService {
	return &CtlService{pty: pty, cfg: cfg}
}

func (c *CtlService) ServiceName() string { return "CtlService" }

func (c *CtlService) ServiceStartup(context.Context, application.ServiceOptions) error {
	s, err := ctl.Listen(ctl.SocketPath(os.Getpid()), c.handle)
	if err != nil {
		return err
	}
	c.server = s
	c.pty.SetSocketPath(s.Path())
	return nil
}

func (c *CtlService) ServiceShutdown() error {
	if c.server != nil {
		return c.server.Close()
	}
	return nil
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
		app.Event.Emit("ctl:open", OpenRequest{Path: p, Line: r.Line, Col: r.Col, Pane: r.Pane, Tab: r.Tab})
		return ctl.Response{OK: true}
	case "new-tab":
		p := r.Path
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			return ctl.Response{Error: "not a directory: " + p}
		}
		app.Event.Emit("ctl:new-tab", OpenRequest{Path: p})
		if w, ok := app.Window.GetByName("main"); ok {
			if w.IsMinimised() {
				w.UnMinimise()
			}
			w.Focus()
		}
		return ctl.Response{OK: true}
	case "input":
		// Writes straight into the PTY of the pane (by WATE_PANE_ID) — no frontend round trip.
		if err := c.pty.WriteToPane(r.Pane, r.Text); err != nil {
			return ctl.Response{Error: err.Error()}
		}
		return ctl.Response{OK: true}
	case "debug":
		// Asks the frontend to log a rendering/state summary (wate ctl debug → see stderr log).
		app.Event.Emit("ctl:action", ActionRequest{Name: "__debug"})
		return ctl.Response{OK: true}
	case "action":
		if strings.TrimSpace(r.Name) == "" {
			return ctl.Response{Error: "action name required"}
		}
		app.Event.Emit("ctl:action", ActionRequest{Name: r.Name, Pane: r.Pane, Tab: r.Tab})
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
		if c.OnHook != nil {
			c.OnHook(ev)
		} else {
			app.Event.Emit("agent:hook", ev)
		}
		return ctl.Response{OK: true}
	default:
		slog.Debug("ctl: unknown command", "cmd", r.Cmd)
		return ctl.Response{Error: "unknown command " + r.Cmd}
	}
}

var errNoPane = errors.New("no such pane")
