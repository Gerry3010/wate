package app

import (
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/wate/internal/config"
)

// ApplyNativeBackground prepares the toolkit window for the configured background mode.
// Translucency is decided at startup (the window is created with it), so this runs once.
func ApplyNativeBackground(cfg config.Config) {
	if cfg.Background.Mode == "translucent" {
		application.InvokeAsync(setWindowTransparent)
	}
}

// ApplyNativeTheme makes the toolkit's own chrome (the GTK title bar on Linux) follow the
// wate theme: dark terminal colours get a dark title bar, light ones a light one.
func ApplyNativeTheme(themes *ThemeService) {
	r, err := themes.Get()
	if err != nil && r.ID == "" {
		return
	}
	dark := isDark(r.Theme.Colors.Background)
	application.InvokeAsync(func() { setPreferDark(dark) })
}

// isDark reports whether a #rrggbb colour is closer to black than to white (perceived luminance).
func isDark(hex string) bool {
	h := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(h) != 6 {
		return true
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return true
	}
	r, g, b := float64(n>>16&0xff), float64(n>>8&0xff), float64(n&0xff)
	return (0.2126*r + 0.7152*g + 0.0722*b) < 128
}
