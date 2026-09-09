package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/Gerry3010/wate/internal/agent"
	"github.com/Gerry3010/wate/internal/app"
	"github.com/Gerry3010/wate/internal/cli"
	"github.com/Gerry3010/wate/internal/config"
	"github.com/Gerry3010/wate/internal/ctl"
	"github.com/Gerry3010/wate/internal/wallpaper"
)

// Frontend build output, embedded into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			fmt.Print(cli.Usage)
			return
		case "open", "ctl", "hook", "install-hooks", "theme":
			os.Exit(cli.Run(os.Args[1:]))
		}
	}
	cfgPath := config.Path()
	initialCwd := ""
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--config" && i+1 < len(args):
			cfgPath = args[i+1]
			i++
		case args[i] == "--cwd" && i+1 < len(args):
			initialCwd = args[i+1]
			i++
		case !strings.HasPrefix(args[i], "-"):
			// `wate <dir>`: open a directory (Nautilus "Open in wate", launchers).
			initialCwd = args[i]
		}
	}
	if initialCwd != "" {
		if abs, err := filepath.Abs(initialCwd); err == nil {
			initialCwd = abs
		}
		if st, err := os.Stat(initialCwd); err != nil || !st.IsDir() {
			initialCwd = filepath.Dir(initialCwd)
		}
		// A running wate (same config) gets a new tab instead of a second window.
		if sock, err := ctl.FindPrimary(config.StateDir()); err == nil {
			if resp, err := ctl.Send(sock, ctl.Request{Cmd: "new-tab", Path: initialCwd}); err == nil && resp.OK {
				return
			}
		}
	}

	cfgSvc := app.NewConfigService(cfgPath)
	cfgSvc.InitialCwd = initialCwd
	// A second wate (first one still running) opens a plain default window and must not
	// touch the first one's saved session or window geometry.
	if _, err := ctl.FindPrimary(config.StateDir()); err == nil {
		cfgSvc.Secondary = true
	}
	ptySvc := app.NewPtyService(cfgSvc.Current)
	ctlSvc := app.NewCtlService(ptySvc, cfgSvc)
	themeSvc := app.NewThemeService(cfgSvc.Current)
	notifySvc := notifications.New()
	agentSvc := app.NewAgentService(ptySvc, ctlSvc, cfgSvc.Current, notifySvc)
	wp := &wallpaper.Handler{Path: func() string { return cfgSvc.Current().Background.Wallpaper }}

	application.RegisterEvent[app.ConfigResponse]("config:changed")
	application.RegisterEvent[app.OpenRequest]("ctl:open")
	application.RegisterEvent[app.ActionRequest]("ctl:action")
	application.RegisterEvent[app.OpenRequest]("ctl:new-tab")
	application.RegisterEvent[app.HookEvent]("agent:hook")
	application.RegisterEvent[agent.Session]("agent:status")

	wapp := application.New(application.Options{
		Name:        "wate",
		Description: "yet another terminal emulator",
		Services: []application.Service{
			application.NewService(cfgSvc),
			application.NewService(ctlSvc),
			application.NewService(ptySvc),
			application.NewService(themeSvc),
			application.NewService(notifySvc),
			application.NewService(agentSvc),
			application.NewService(&app.LogService{}),
			application.NewService(&app.OpenerService{}),
			application.NewService(&app.FileService{}),
			application.NewService(&app.StateService{}),
			application.NewService(&app.SessionService{}),
			application.NewService(app.NewImportService(cfgSvc)),
			application.NewServiceWithOptions(wp, application.ServiceOptions{Name: "Wallpaper", Route: "/wallpaper"}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	var saved *app.WindowState
	if cfg := cfgSvc.Current(); cfg.Window.RememberSize && !cfgSvc.Secondary {
		if st, ok := app.LoadWindowState(); ok {
			saved = &st
		}
	}
	win := wapp.Window.NewWithOptions(app.WindowOptions(cfgSvc.Current(), saved))
	if !cfgSvc.Secondary {
		app.TrackWindow(win)
	}
	wapp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		app.ApplyNativeTheme(themeSvc)
		app.ApplyNativeBackground(cfgSvc.Current())
	})
	cfgSvc.OnChange = func() { app.ApplyNativeTheme(themeSvc) }

	stopWatch, err := app.WatchConfig(cfgPath, func() { cfgSvc.Reload() })
	if err != nil {
		log.Println("config watch disabled:", err)
	} else {
		defer stopWatch()
	}

	if err := wapp.Run(); err != nil {
		log.Fatal(err)
	}
}
