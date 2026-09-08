package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/yate/internal/config"
)

// WindowOptions derives the main window options from the background config.
func WindowOptions(cfg config.Config) application.WebviewWindowOptions {
	opts := application.WebviewWindowOptions{
		Name:      "main",
		Title:     "yate",
		Width:     1100,
		Height:    700,
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
