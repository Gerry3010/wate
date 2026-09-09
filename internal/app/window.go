package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/wate/internal/config"
)

// WindowOptions derives the main window options from the config; saved (when non-nil) is the
// remembered geometry of the last run and wins over the configured default size.
func WindowOptions(cfg config.Config, saved *WindowState) application.WebviewWindowOptions {
	opts := application.WebviewWindowOptions{
		Name:      "main",
		Title:     "wate",
		Width:     cfg.Window.Width,
		Height:    cfg.Window.Height,
		MinWidth:  400,
		MinHeight: 240,
		URL:       "/",
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 36,
			TitleBar:                application.MacTitleBarHiddenInset,
			Backdrop:                application.MacBackdropNormal,
		},
		Linux: application.LinuxWindow{
			WebviewGpuPolicy: application.WebviewGpuPolicyAlways,
		},
		BackgroundType:   application.BackgroundTypeSolid,
		BackgroundColour: application.NewRGB(30, 30, 46),
	}
	if saved != nil {
		opts.Width, opts.Height = saved.Width, saved.Height
		opts.X, opts.Y = saved.X, saved.Y
		opts.InitialPosition = application.WindowXY
		if saved.Maximised {
			opts.StartState = application.WindowStateMaximised
		}
	}
	switch cfg.Background.Mode {
	case "translucent":
		// The OS/compositor blurs whatever is behind the window.
		opts.BackgroundType = application.BackgroundTypeTranslucent
		opts.BackgroundColour = application.NewRGBA(30, 30, 46, 0)
		opts.Mac.Backdrop = application.MacBackdropTranslucent
		opts.Linux.WindowIsTranslucent = true
	case "wallpaper":
		// We paint the (blurred) wallpaper ourselves; the window itself stays opaque.
		opts.BackgroundType = application.BackgroundTypeSolid
	}
	return opts
}
