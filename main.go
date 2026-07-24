package main

import (
	"embed"
	"fmt"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed all:data
var builtinTemplates embed.FS

func main() {
	application := NewApp()
	err := wails.Run(&options.App{
		Title:            "expand",
		Width:            520,
		Height:           620,
		MinWidth:         440,
		MinHeight:        520,
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 233, G: 237, B: 242, A: 1},
		Assets:           assets,
		OnStartup:        application.startup,
		OnShutdown:       application.shutdown,
		OnBeforeClose:    application.beforeClose,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.florune.expand",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				application.log.Info("app.second_instance", "second launch redirected to the running instance")
				application.OpenCompact()
			},
		},
		Bind:                     []interface{}{application},
		EnableDefaultContextMenu: false,
	})
	if err != nil {
		application.log.Error("app.run", err)
		fmt.Println("expand stopped:", err)
	}
}
