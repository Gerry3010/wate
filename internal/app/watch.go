package app

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchConfig reloads the config (and thereby theme) when config.toml or a theme file changes.
// Editors write via rename/truncate, so watch the directories and debounce.
func WatchConfig(cfgPath string, reload func()) (func() error, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	dirs := []string{filepath.Dir(cfgPath), userThemesDir()}
	for _, d := range dirs {
		_ = os.MkdirAll(d, 0o755)
		if err := w.Add(d); err != nil {
			slog.Warn("watch", "dir", d, "err", err)
		}
	}
	go func() {
		var timer *time.Timer
		for {
			select {
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if !strings.HasSuffix(ev.Name, ".toml") {
					continue
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(150*time.Millisecond, reload)
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				slog.Warn("watch", "err", err)
			}
		}
	}()
	return w.Close, nil
}
