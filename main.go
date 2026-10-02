package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

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
	newWindow, standalone := false, false
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--new-window":
			newWindow = true
		case args[i] == "--standalone":
			standalone = true
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
	}

	// A running wate (same config) takes this over: a new tab by default, a window on request.
	// --standalone is the way out, for debugging and for a throwaway WATE_CONFIG_DIR.
	if !standalone && (initialCwd != "" || newWindow) {
		if sock, err := ctl.FindPrimary(config.StateDir()); err == nil {
			cmd := ctl.Request{Cmd: "new-tab", Path: initialCwd}
			if newWindow {
				cmd = ctl.Request{Cmd: "new-window", Path: initialCwd}
			}
			if resp, err := ctl.Send(sock, cmd); err == nil && resp.OK {
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
	winSvc := app.NewWindowService(cfgSvc)
	stateSvc := app.NewStateService(winSvc)
	ptySvc := app.NewPtyService(cfgSvc.Current, winSvc)
	ctlSvc := app.NewCtlService(ptySvc, cfgSvc, winSvc)
	themeSvc := app.NewThemeService(cfgSvc.Current)
	notifySvc := notifications.New()
	agentSvc := app.NewAgentService(ptySvc, ctlSvc, cfgSvc.Current, notifySvc)
	// Quitting: hand the primary role to whoever starts next, then let Claude Code shut down
	// before the shells get their SIGHUP.
	winSvc.OnClosed = func(key string) { stateSvc.Remove(key) }
	ptySvc.BeforeKill = func(ctx context.Context) {
		// Shutting down: every window is about to close, and they must keep their tabs.
		winSvc.MarkQuitting()
		ctlSvc.Resign()
		agentSvc.StopSessions(ctx)
	}
	wp := &wallpaper.Handler{Path: func() string { return cfgSvc.Current().Background.Wallpaper }}

	application.RegisterEvent[app.ConfigResponse]("config:changed")
	application.RegisterEvent[app.OpenRequest]("ctl:open")
	application.RegisterEvent[app.ActionRequest]("ctl:action")
	application.RegisterEvent[app.OpenRequest]("ctl:new-tab")
	application.RegisterEvent[agent.Session]("agent:status")
	application.RegisterEvent[app.ActivateTab]("window:activate-tab")
	application.RegisterEvent[app.DropRequest]("window:drop")
	application.RegisterEvent[app.FullscreenState]("window:fullscreen")

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
			application.NewService(stateSvc),
			application.NewService(&app.SessionService{}),
			application.NewService(&app.AgentStateService{}),
			application.NewService(winSvc),
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

	// Reopen the windows of the last run. A window finds its tabs by state key, so the key has
	// to come from the file rather than from the id minted this time round.
	keys := []string{"w-1"}
	if !cfgSvc.Secondary {
		if blobs := stateSvc.LoadAll(); len(blobs) > 0 {
			keys = keys[:0]
			for _, b := range blobs {
				keys = append(keys, b.Key)
			}
		}
	}
	restore := "session"
	if cfgSvc.Secondary {
		restore = "empty"
		keys = []string{"w-1"}
	}
	for i, key := range keys {
		var saved *app.WindowState
		if cfg := cfgSvc.Current(); cfg.Window.RememberSize && !cfgSvc.Secondary {
			if st, ok := app.LoadWindowStateFor(key); ok {
				saved = &st
			}
		}
		o := app.OpenWindowOptions{
			Geometry: saved, Restore: restore, StateKey: key,
			Track:   !cfgSvc.Secondary,
			Persist: !cfgSvc.Secondary,
		}
		// The directory from the command line belongs to the first window only.
		if i == 0 {
			o.Cwd = initialCwd
		}
		if _, err := winSvc.Open(o); err != nil {
			log.Fatalln("could not open a window:", err)
		}
	}
	wapp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		winSvc.MarkStarted()
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

	// A SIGINT/SIGTERM (logout, `kill`, a crashing launcher) quits like closing the window
	// does, so the shutdown sequence still runs: Claude sessions first, then the shells.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		wapp.Quit()
	}()

	if err := wapp.Run(); err != nil {
		log.Fatal(err)
	}
}
