package app

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestDropRequestFrom(t *testing.T) {
	paths := []string{"/tmp/a.png", "/tmp/b.txt"}

	t.Run("carries the point the drop landed on", func(t *testing.T) {
		got := DropRequestFrom(paths, &application.DropTargetDetails{
			X:          120,
			Y:          340,
			Attributes: map[string]string{"data-pane-id": "pane-2-abc", "data-file-drop-target": ""},
		})
		if got.X != 120 || got.Y != 340 {
			t.Fatalf("got %+v", got)
		}
		if len(got.Paths) != 2 || got.Paths[0] != paths[0] {
			t.Fatalf("paths not carried: %+v", got.Paths)
		}
	})

	t.Run("survives a drop with no target element", func(t *testing.T) {
		got := DropRequestFrom(paths, nil)
		if got.X != 0 || got.Y != 0 {
			t.Fatalf("got %+v", got)
		}
		if len(got.Paths) != 2 {
			t.Fatalf("paths not carried: %+v", got.Paths)
		}
	})

	t.Run("keeps the coordinates of a bare target", func(t *testing.T) {
		got := DropRequestFrom(paths, &application.DropTargetDetails{X: 5, Y: 6})
		if got.X != 5 || got.Y != 6 {
			t.Fatalf("got %+v", got)
		}
	})
}
