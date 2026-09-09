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

// LoadWindowState returns the remembered geometry, ok=false when none is usable.
func LoadWindowState() (WindowState, bool) {
	data, err := os.ReadFile(windowStatePath())
	if err != nil {
		return WindowState{}, false
	}
	var s WindowState
	if json.Unmarshal(data, &s) != nil || s.Width < 200 || s.Height < 120 {
		return WindowState{}, false
	}
	return s, true
}

func saveWindowState(s WindowState) {
	p := windowStatePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	data, _ := json.Marshal(s)
	if err := os.WriteFile(p+".tmp", data, 0o600); err == nil {
		_ = os.Rename(p+".tmp", p)
	} else {
		slog.Debug("window state", "err", err)
	}
}

// TrackWindow remembers size, position and maximised state whenever they change (debounced).
func TrackWindow(w *application.WebviewWindow) {
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
				if prev, ok := LoadWindowState(); ok {
					st.X, st.Y, st.Width, st.Height = prev.X, prev.Y, prev.Width, prev.Height
				}
			}
			saveWindowState(st)
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
