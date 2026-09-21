package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// FullscreenState is a fullscreen transition of the window.
//
// The frontend cannot see this by itself: the webview fills the window either way, and the
// document never enters the Fullscreen API. It matters because the macOS tab bar keeps its
// left edge clear of the traffic lights, which are not there in fullscreen — see .tabbar in
// frontend/src/style/base.css.
type FullscreenState struct {
	Fullscreen bool `json:"fullscreen"`
}

// ForwardFullscreen relays the window's fullscreen transitions to the frontend as
// "window:fullscreen".
func ForwardFullscreen(w *application.WebviewWindow) {
	emit := func(on bool) {
		if app := application.Get(); app != nil {
			app.Event.Emit("window:fullscreen", FullscreenState{Fullscreen: on})
		}
	}
	w.OnWindowEvent(events.Common.WindowFullscreen, func(*application.WindowEvent) { emit(true) })
	w.OnWindowEvent(events.Common.WindowUnFullscreen, func(*application.WindowEvent) { emit(false) })
}
