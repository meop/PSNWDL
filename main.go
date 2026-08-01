package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"PSNWDL/internal/activity"
	"PSNWDL/internal/config"
	"PSNWDL/internal/downloads"
	"PSNWDL/internal/jobs"
	"PSNWDL/internal/library"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[activity.Entry]("activity:log")
	application.RegisterEvent[config.Config]("config:updated")
	application.RegisterEvent[[]downloads.Title]("downloads:updated")
	application.RegisterEvent[string]("downloads:error")
	application.RegisterEvent[jobs.Job](jobs.EventJobAdded)
	application.RegisterEvent[jobs.Job](jobs.EventJobProgress)
	application.RegisterEvent[jobs.Job](jobs.EventJobState)
	application.RegisterEvent[[]library.Row]("library:updated")
	application.RegisterEvent[string]("library:error")
}

func main() {
	wailsApp := application.New(application.Options{
		Name:        "PSNWDL",
		Description: "PlayStation Network update and firmware download manager",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	service := NewApp(wailsApp)
	wailsApp.RegisterService(application.NewService(service))

	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            " ",
		Width:            1280,
		Height:           720,
		MinWidth:         1280,
		MinHeight:        720,
		BackgroundColour: application.NewRGB(15, 20, 25),
		URL:              "/",
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}
