package main

import (
	"embed"
	"fmt"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Gerry3010/yate/internal/app"
	"github.com/Gerry3010/yate/internal/config"
)

// Frontend build output, embedded into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			fmt.Println("yate — yet another terminal emulator\n\nusage: yate [--config <file>]")
			return
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

	application.RegisterEvent[app.ConfigResponse]("config:changed")

	wapp := application.New(application.Options{
		Name:        "yate",
		Description: "yet another terminal emulator",
		Services: []application.Service{
			application.NewService(cfgSvc),
			application.NewService(ptySvc),
			application.NewService(&app.LogService{}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	wapp.Window.NewWithOptions(app.WindowOptions(cfgSvc.Current()))

	if err := wapp.Run(); err != nil {
		log.Fatal(err)
	}
}
