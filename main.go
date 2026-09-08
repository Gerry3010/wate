package main

import (
	"embed"
	"fmt"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/yate/internal/app"
	"github.com/Gerry3010/yate/internal/cli"
	"github.com/Gerry3010/yate/internal/config"
	"github.com/Gerry3010/yate/internal/wallpaper"
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
		case "open", "ctl", "hook", "install-hooks":
			os.Exit(cli.Run(os.Args[1:]))
		}
	}
	cfgPath := config.Path()
	for i, a := range os.Args {
		if a == "--config" && i+1 < len(os.Args) {
			cfgPath = os.Args[i+1]
		}
	}

	cfgSvc := app.NewConfigService(cfgPath)
	ptySvc := app.NewPtyService(cfgSvc.Current)
	ctlSvc := app.NewCtlService(ptySvc)
	themeSvc := app.NewThemeService(cfgSvc.Current)
	wp := &wallpaper.Handler{Path: func() string { return cfgSvc.Current().Background.Wallpaper }}

	application.RegisterEvent[app.ConfigResponse]("config:changed")
	application.RegisterEvent[app.OpenRequest]("ctl:open")
	application.RegisterEvent[app.ActionRequest]("ctl:action")
	application.RegisterEvent[app.HookEvent]("agent:hook")

	wapp := application.New(application.Options{
		Name:        "yate",
		Description: "yet another terminal emulator",
		Services: []application.Service{
			application.NewService(cfgSvc),
			application.NewService(ctlSvc),
			application.NewService(ptySvc),
			application.NewService(themeSvc),
			application.NewService(&app.LogService{}),
			application.NewService(&app.OpenerService{}),
			application.NewService(&app.FileService{}),
			application.NewServiceWithOptions(wp, application.ServiceOptions{Name: "Wallpaper", Route: "/wallpaper"}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	wapp.Window.NewWithOptions(app.WindowOptions(cfgSvc.Current()))

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
