package app

import "github.com/wailsapp/wails/v3/pkg/application"

// emitTo sends an event to one window. The frontend reads the window's name off the event and
// ignores anything addressed elsewhere. A nil window falls back to an app-wide broadcast, so a
// message is never silently dropped while a window is going away.
func emitTo(w *application.WebviewWindow, name string, data any) {
	if w != nil {
		w.EmitEvent(name, data)
		return
	}
	if a := application.Get(); a != nil {
		a.Event.Emit(name, data)
	}
}
