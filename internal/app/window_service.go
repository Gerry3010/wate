package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

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
	// Persist is whether this window writes its tabs back. Restoring and saving are separate:
	// a window opened fresh has nothing to restore but must still come back next time.
	Persist bool `json:"persist"`
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

// TabTransfer moves one tab to another window. Tab is the frontend's saved shape; Live maps
// each pane id to the PTY session it already has, so the receiving window re-attaches instead
// of spawning and the shells survive the move.
type TabTransfer struct {
	// To is the target window id; empty means "a window of its own".
	To    string            `json:"to"`
	Index int               `json:"index"`
	TabID string            `json:"tab_id"`
	Tab   map[string]any    `json:"tab"`
	Live  map[string]string `json:"live"`
}

// OpenWindowOptions describes a window to create.
type OpenWindowOptions struct {
	Cwd      string
	Geometry *WindowState
	Restore  string
	StateKey string
	// Track remembers this window's geometry between runs; Persist, its tabs.
	Track   bool
	Persist bool
	// Adopt is a tab arriving from another window, handed to the frontend on Bootstrap.
	Adopt *TabTransfer
}

type winEntry struct {
	id   WindowID
	win  *application.WebviewWindow
	boot Bootstrap
	// adopt is the tab this window was opened to receive, consumed once by the frontend.
	adopt *TabTransfer
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
	// moving maps a tab being handed over to the window still holding it, so the source can
	// be told to let go — but only once the target says it has the tab.
	moving  map[string]WindowID
	focused WindowID
	// quitting is set while the app shuts down. Every window closes then, and a window that
	// closes on the way out must keep its saved tabs — only one the user closed loses them.
	quitting bool
	// OnClosed is called with the state key of a window the user closed, so its slot can go.
	OnClosed func(key string)
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
		moving:     map[string]WindowID{},
	}
}

func (*WindowService) ServiceName() string { return "WindowService" }

// MarkQuitting says the app is shutting down, so closing windows keep their saved tabs.
func (s *WindowService) MarkQuitting() {
	s.mu.Lock()
	s.quitting = true
	s.mu.Unlock()
}

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
		// Not the window id: that is minted per run and would collide with an older slot.
		key = "win-" + randomKey()
	}
	boot := Bootstrap{WindowID: string(id), InitialCwd: o.Cwd, Restore: o.Restore, StateKey: key, Persist: o.Persist}
	if boot.Restore == "" {
		boot.Restore = "empty"
	}
	entry := &winEntry{id: id, boot: boot, adopt: o.Adopt}
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
		TrackWindow(win, key)
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
	e, known := s.windows[id]
	drop := ""
	if known && !s.quitting && e.boot.Persist {
		drop = e.boot.StateKey
	}
	onClosed := s.OnClosed
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
	s.mu.Unlock()
	// A window the user closed should not come back on the next start.
	if drop != "" && onClosed != nil {
		onClosed(drop)
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
	id, err := s.Open(OpenWindowOptions{Cwd: cwd, Restore: "empty", Track: true, Persist: true})
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
// TransferTab hands a tab to another window, or to a new one when To is empty.
//
// Nothing is destroyed here. The target is asked to adopt, and only when it confirms (see
// TabAdopted) is the source told to let go — so a failure anywhere leaves the tab where it is.
func (s *WindowService) TransferTab(ctx context.Context, t TabTransfer) error {
	from := s.idFromContext(ctx)
	if t.TabID == "" {
		return fmt.Errorf("tab id required")
	}
	s.mu.Lock()
	s.moving[t.TabID] = from
	// Claim the tab for its new owner right away: between now and the source's next report
	// it must not look unowned.
	if t.To != "" {
		s.tabWindow[t.TabID] = WindowID(t.To)
	}
	s.mu.Unlock()

	if t.To == "" {
		if _, err := s.Open(OpenWindowOptions{Restore: "adopt", Track: true, Persist: true, Adopt: &t}); err != nil {
			s.cancelMove(t.TabID)
			return err
		}
		return nil
	}

	s.mu.RLock()
	e, ok := s.windows[WindowID(t.To)]
	s.mu.RUnlock()
	if !ok || e.win == nil {
		s.cancelMove(t.TabID)
		return fmt.Errorf("no window %s", t.To)
	}
	application.InvokeSync(func() { e.win.Focus() })
	e.win.EmitEvent("window:adopt-tab", t)
	return nil
}

// TabAdopted is the receiving window confirming it has the tab. Only now is the window that
// had it told to let go.
func (s *WindowService) TabAdopted(ctx context.Context, tabID string) {
	to := s.idFromContext(ctx)
	s.mu.Lock()
	from, ok := s.moving[tabID]
	delete(s.moving, tabID)
	if ok {
		s.tabWindow[tabID] = to
	}
	e, known := s.windows[from]
	s.mu.Unlock()
	if ok && known && e.win != nil && from != to {
		e.win.EmitEvent("window:release-tab", ActivateTab{Tab: tabID})
	}
}

// PendingTab is the tab a window was opened to receive; it is handed over once.
func (s *WindowService) PendingTab(ctx context.Context) *TabTransfer {
	id := s.idFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.windows[id]
	if !ok || e.adopt == nil {
		return nil
	}
	t := e.adopt
	e.adopt = nil
	return t
}

// WindowAt is the id of the window under a point given in the CALLING window's client
// coordinates, "" when the point is over none of them — which is what makes dropping a tab on
// the desktop mean "a new window". Later windows win, so the topmost of a stack is reported.
//
// The conversion to screen coordinates happens here because the WebView cannot do it: its
// PointerEvent.screenX is relative to the window, not the screen.
func (s *WindowService) WindowAt(ctx context.Context, x, y int) string {
	from := s.idFromContext(ctx)
	s.mu.RLock()
	order := append([]WindowID(nil), s.order...)
	wins := make(map[WindowID]*application.WebviewWindow, len(order))
	for id, e := range s.windows {
		wins[id] = e.win
	}
	s.mu.RUnlock()

	hit := ""
	application.InvokeSync(func() {
		sx, sy := x, y
		if src := wins[from]; src != nil {
			ox, oy := src.Position()
			sx, sy = ox+x, oy+y
		}
		for _, id := range order {
			w := wins[id]
			if w == nil {
				continue
			}
			wx, wy := w.Position()
			ww, wh := w.Size()
			if sx >= wx && sx < wx+ww && sy >= wy && sy < wy+wh {
				hit = string(id)
			}
		}
	})
	return hit
}

// Windows lists the open windows for a "move to window" menu, newest last.
func (s *WindowService) Windows() []WindowInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]WindowInfo, 0, len(s.order))
	for i, id := range s.order {
		e, ok := s.windows[id]
		if !ok {
			continue
		}
		title := ""
		if e.win != nil {
			title = e.win.Name()
		}
		out = append(out, WindowInfo{ID: string(id), Title: title, Index: i + 1})
	}
	return out
}

// WindowInfo names a window for the UI.
type WindowInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Index int    `json:"index"`
}

func (s *WindowService) cancelMove(tabID string) {
	s.mu.Lock()
	delete(s.moving, tabID)
	s.mu.Unlock()
}

func (s *WindowService) idFromContext(ctx context.Context) WindowID {
	if w, ok := ctx.Value(application.WindowKey).(application.Window); ok && w != nil {
		return WindowID(w.Name())
	}
	return ""
}

// randomKey is a short, collision-resistant suffix for a new window's state key.
func randomKey() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
