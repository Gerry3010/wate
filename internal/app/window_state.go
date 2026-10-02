package app

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/Gerry3010/wate/internal/config"
)

// WindowState is the last known geometry of the main window (remembered between runs).
type WindowState struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximised bool `json:"maximised"`
}

func windowStatePath() string { return filepath.Join(config.StateDir(), "window.json") }

// WindowRecord is one window's remembered geometry. Key is stable across runs, unlike the
// WindowID, which is minted per process.
type WindowRecord struct {
	Key   string      `json:"key"`
	State WindowState `json:"state"`
}

type windowsFile struct {
	Version int            `json:"version"`
	Windows []WindowRecord `json:"windows"`
}

func usable(s WindowState) bool { return s.Width >= 200 && s.Height >= 120 }

// readWindows reads window.json. A v1 file is a bare WindowState with no version field, so
// unmarshalling into the v2 shape leaves Version at 0 — a reliable way to tell them apart.
func readWindows() windowsFile {
	data, err := os.ReadFile(windowStatePath())
	if err != nil {
		return windowsFile{Version: 2}
	}
	var f windowsFile
	if json.Unmarshal(data, &f) == nil && f.Version == 2 {
		return f
	}
	var one WindowState
	if json.Unmarshal(data, &one) == nil && usable(one) {
		return windowsFile{Version: 2, Windows: []WindowRecord{{Key: "w-1", State: one}}}
	}
	return windowsFile{Version: 2}
}

// LoadWindowState returns the first remembered geometry — the one a single-window start uses.
func LoadWindowState() (WindowState, bool) {
	for _, r := range readWindows().Windows {
		if usable(r.State) {
			return r.State, true
		}
	}
	return WindowState{}, false
}

// LoadWindowStateFor returns one window's remembered geometry by key.
func LoadWindowStateFor(key string) (WindowState, bool) {
	for _, r := range readWindows().Windows {
		if r.Key == key && usable(r.State) {
			return r.State, true
		}
	}
	return WindowState{}, false
}

// geometry is the single writer of window.json. Several windows each run their own debounce;
// letting them all read-modify-write the file would lose updates.
var geometry = &windowStore{}

type windowStore struct {
	mu sync.Mutex
}

func (g *windowStore) put(key string, st WindowState) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f := readWindows()
	for i := range f.Windows {
		if f.Windows[i].Key == key {
			f.Windows[i].State = st
			writeWindows(f)
			return
		}
	}
	f.Windows = append(f.Windows, WindowRecord{Key: key, State: st})
	writeWindows(f)
}

func (g *windowStore) remove(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f := readWindows()
	out := f.Windows[:0]
	for _, r := range f.Windows {
		if r.Key != key {
			out = append(out, r)
		}
	}
	f.Windows = out
	writeWindows(f)
}

func (g *windowStore) get(key string) (WindowState, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return LoadWindowStateFor(key)
}

func writeWindows(f windowsFile) {
	p := windowStatePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	f.Version = 2
	data, _ := json.Marshal(f)
	if err := os.WriteFile(p+".tmp", data, 0o600); err == nil {
		_ = os.Rename(p+".tmp", p)
	} else {
		slog.Debug("window state", "err", err)
	}
}

// TrackWindow remembers a window's size, position and maximised state whenever they change
// (debounced per window; the file itself has one writer).
func TrackWindow(w *application.WebviewWindow, key string) {
	var mu sync.Mutex
	var timer *time.Timer
	var maximised bool
	capture := func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(400*time.Millisecond, func() {
			mu.Lock()
			max := maximised
			mu.Unlock()
			st := WindowState{Maximised: max}
			application.InvokeSync(func() {
				st.Width, st.Height = w.Size()
				st.X, st.Y = w.Position()
			})
			if max {
				// Keep the last normal geometry so un-maximising later lands somewhere sensible.
				if prev, ok := geometry.get(key); ok {
					st.X, st.Y, st.Width, st.Height = prev.X, prev.Y, prev.Width, prev.Height
				}
			}
			geometry.put(key, st)
		})
	}
	w.OnWindowEvent(events.Common.WindowDidResize, func(*application.WindowEvent) { capture() })
	w.OnWindowEvent(events.Common.WindowDidMove, func(*application.WindowEvent) { capture() })
	w.OnWindowEvent(events.Common.WindowMaximise, func(*application.WindowEvent) {
		mu.Lock()
		maximised = true
		mu.Unlock()
		capture()
	})
	w.OnWindowEvent(events.Common.WindowUnMaximise, func(*application.WindowEvent) {
		mu.Lock()
		maximised = false
		mu.Unlock()
		capture()
	})
}
