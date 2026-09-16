package app

import (
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// DropRequest is a file dropped onto the window by the OS: the paths, and where they landed in
// CSS pixels. Which pane that is, and what should happen there, is the frontend's business.
//
// The webview cannot work this out by itself. WebKitGTK reports a file drop with an empty
// DataTransfer — `files` is empty and `getData` answers "" for every type it advertises — so the
// paths exist only on the native side, where Wails' drop target reads them from the file list.
type DropRequest struct {
	Paths []string `json:"paths"`
	X     int      `json:"x"`
	Y     int      `json:"y"`
}

// DropRequestFrom assembles the request from what Wails reports for a WindowFilesDropped event.
// The details are nil when nothing carrying the drop-target attribute was under the pointer.
func DropRequestFrom(paths []string, d *application.DropTargetDetails) DropRequest {
	req := DropRequest{Paths: paths}
	if d != nil {
		req.X, req.Y = d.X, d.Y
	}
	return req
}

// ForwardFileDrops relays the window's OS file drops to the frontend as "window:drop".
func ForwardFileDrops(w *application.WebviewWindow) {
	w.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		ctx := e.Context()
		paths := ctx.DroppedFiles()
		if len(paths) == 0 {
			return
		}
		req := DropRequestFrom(paths, ctx.DropTargetDetails())
		slog.Debug("file drop", "files", len(paths), "x", req.X, "y", req.Y)
		// The listener runs on Wails' drop goroutine (a buffered channel): emit and get out.
		if app := application.Get(); app != nil {
			app.Event.Emit("window:drop", req)
		}
	})
}
