package app

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// WindowID names a window. It is also the Wails window name, deliberately: bound calls are
// attributed by name (the message processor looks the window up with GetByName), events carry
// it as their sender, and the frontend reads it back with Window.Name(). One identifier for
// all three means they cannot disagree. Ids are never reused within a run.
type WindowID string

// Bootstrap is what a window needs to know about itself at startup. It replaces the per-window
// fields that used to sit on ConfigService, whose payload is also broadcast as config:changed —
// where anything window-specific is actively misleading.
type Bootstrap struct {
	WindowID string `json:"window_id"`
	// InitialCwd is the directory given on the command line, for this window's first tab.
	InitialCwd string `json:"initial_cwd"`
	// Restore is "session" (load this window's saved tabs), "empty", or "adopt" (a tab is
	// arriving from another window).
	Restore string `json:"restore"`
	// StateKey indexes this window's slot in the session and geometry files. It is stable
	// across runs, unlike WindowID, which is minted per process.
	StateKey string `json:"state_key"`
}

// ActivateTab asks a window to bring a tab (and optionally a pane) to the front.
type ActivateTab struct {
	Tab  string `json:"tab"`
	Pane string `json:"pane"`
}

// WindowTabs is a window reporting what it currently holds. The frontend is the only place
// that knows which tabs live in which window, so it has to say.
type WindowTabs struct {
	Tabs  []string `json:"tabs"`
	Panes []string `json:"panes"`
}

// OpenWindowOptions describes a window to create.
type OpenWindowOptions struct {
	Cwd      string
	Geometry *WindowState
	Restore  string
	StateKey string
	// Track remembers this window's geometry between runs.
	Track bool
}

type winEntry struct {
	id   WindowID
	win  *application.WebviewWindow
	boot Bootstrap
}

// WindowService owns the set of open windows and the mapping from tabs and panes to them.
type WindowService struct {
	cfg *ConfigService

	mu         sync.RWMutex
	seq        int
	windows    map[WindowID]*winEntry
	order      []WindowID
	tabWindow  map[string]WindowID
	paneWindow map[string]WindowID
	focused    WindowID
	// started is false until the event loop runs. The first window is created on the main
	// thread before Run(), where marshalling onto it would deadlock.
	started bool
}

func NewWindowService(cfg *ConfigService) *WindowService {
	return &WindowService{
		cfg:        cfg,
		windows:    map[WindowID]*winEntry{},
		tabWindow:  map[string]WindowID{},
		paneWindow: map[string]WindowID{},
	}
}

func (*WindowService) ServiceName() string { return "WindowService" }

// MarkStarted says the event loop is up, so later windows must be created on the main thread.
func (s *WindowService) MarkStarted() {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
}

// Open creates a window and registers it. Safe to call from any goroutine: window creation is
// marshalled onto the main thread, which GTK requires.
func (s *WindowService) Open(o OpenWindowOptions) (WindowID, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("no application")
	}
	s.mu.Lock()
	s.seq++
	id := WindowID("w" + strconv.Itoa(s.seq))
	key := o.StateKey
	if key == "" {
		key = string(id)
	}
	boot := Bootstrap{WindowID: string(id), InitialCwd: o.Cwd, Restore: o.Restore, StateKey: key}
	if boot.Restore == "" {
		boot.Restore = "empty"
	}
	entry := &winEntry{id: id, boot: boot}
	s.windows[id] = entry
	s.order = append(s.order, id)
	if s.focused == "" {
		s.focused = id
	}
	started := s.started
	s.mu.Unlock()

	opts := WindowOptions(s.cfg.Current(), o.Geometry, string(id))
	var win *application.WebviewWindow
	if started {
		application.InvokeSync(func() { win = app.Window.NewWithOptions(opts) })
	} else {
		win = app.Window.NewWithOptions(opts)
	}
	if win == nil {
		s.mu.Lock()
		delete(s.windows, id)
		s.order = s.order[:len(s.order)-1]
		s.mu.Unlock()
		return "", fmt.Errorf("window %s was not created", id)
	}

	s.mu.Lock()
	entry.win = win
	s.mu.Unlock()
	s.track(id, win)
	if o.Track {
		TrackWindow(win)
	}
	ForwardFileDrops(win)
	ForwardFullscreen(win)
	slog.Debug("window opened", "id", id, "restore", boot.Restore, "key", key)
	return id, nil
}

// track keeps the focus pointer current and forgets a window once it closes.
func (s *WindowService) track(id WindowID, win *application.WebviewWindow) {
	win.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) {
		s.mu.Lock()
		s.focused = id
		s.mu.Unlock()
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) { s.forget(id) })
}

func (s *WindowService) forget(id WindowID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.windows, id)
	for i, w := range s.order {
		if w == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	for tab, w := range s.tabWindow {
		if w == id {
			delete(s.tabWindow, tab)
		}
	}
	for pane, w := range s.paneWindow {
		if w == id {
			delete(s.paneWindow, pane)
		}
	}
	if s.focused == id {
		s.focused = ""
		if len(s.order) > 0 {
			s.focused = s.order[0]
		}
	}
}

// Bootstrap hands a window its own startup facts. Wails fills ctx with the calling window.
func (s *WindowService) Bootstrap(ctx context.Context) Bootstrap {
	id := s.idFromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.windows[id]; ok {
		return e.boot
	}
	return Bootstrap{WindowID: string(id), Restore: "empty", StateKey: string(id)}
}

// SetTabs records which tabs and panes a window holds. Sent whenever the layout changes, so
// the backend can route a ctl request or a hook back to the right window.
func (s *WindowService) SetTabs(ctx context.Context, t WindowTabs) {
	id := s.idFromContext(ctx)
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Claim first, release second: a tab moving between windows is claimed by its new owner
	// before the old one reports its loss, so it is never briefly owned by nobody.
	for tab, w := range s.tabWindow {
		if w == id && !contains(t.Tabs, tab) {
			delete(s.tabWindow, tab)
		}
	}
	for pane, w := range s.paneWindow {
		if w == id && !contains(t.Panes, pane) {
			delete(s.paneWindow, pane)
		}
	}
	for _, tab := range t.Tabs {
		s.tabWindow[tab] = id
	}
	for _, pane := range t.Panes {
		s.paneWindow[pane] = id
	}
}

// windowFor picks the window a request is about: the pane's, else the tab's, else whichever
// has the focus, else the only one there is.
func (s *WindowService) windowFor(paneID, tabID string) (*application.WebviewWindow, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range []WindowID{s.paneWindow[paneID], s.tabWindow[tabID], s.focused} {
		if id == "" {
			continue
		}
		if e, ok := s.windows[id]; ok && e.win != nil {
			return e.win, true
		}
	}
	if len(s.order) == 1 {
		if e, ok := s.windows[s.order[0]]; ok && e.win != nil {
			return e.win, true
		}
	}
	return nil, false
}

// windowForTab is windowFor as a plain id, for stamping onto an agent session.
func (s *WindowService) windowForTab(tabID, paneID string) WindowID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if id, ok := s.paneWindow[paneID]; ok {
		return id
	}
	return s.tabWindow[tabID]
}

// count is how many windows are open.
func (s *WindowService) count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.order)
}

// NewWindow opens an empty window (the new_window action, and `wate --new-window`).
func (s *WindowService) NewWindow(ctx context.Context, cwd string) (string, error) {
	id, err := s.Open(OpenWindowOptions{Cwd: cwd, Restore: "empty"})
	return string(id), err
}

// FocusPane raises the window holding a tab and asks it to show that pane — the sidebar lists
// sessions from every window, so clicking one has to be able to cross windows.
func (s *WindowService) FocusPane(windowID, tabID, paneID string) error {
	s.mu.RLock()
	e, ok := s.windows[WindowID(windowID)]
	s.mu.RUnlock()
	if !ok || e.win == nil {
		return fmt.Errorf("no window %s", windowID)
	}
	win := e.win
	application.InvokeSync(func() {
		if win.IsMinimised() {
			win.UnMinimise()
		}
		win.Focus()
	})
	win.EmitEvent("window:activate-tab", ActivateTab{Tab: tabID, Pane: paneID})
	return nil
}

// idFromContext reads the calling window off the context. Wails puts it there for any bound
// method whose first parameter is a context.Context; the TypeScript generator strips that
// parameter, so the frontend signature is unchanged and no window has to name itself.
func (s *WindowService) idFromContext(ctx context.Context) WindowID {
	if w, ok := ctx.Value(application.WindowKey).(application.Window); ok && w != nil {
		return WindowID(w.Name())
	}
	return ""
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
