//go:build windows

package main

import (
	"embed"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"khutaf/internal/updater"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if handled, code := updater.CheckApplyFlag(os.Args[1:]); handled {
		os.Exit(code)
	}

	app := NewApp()
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "--updated-from=") {
			app.updatedFrom = strings.TrimPrefix(arg, "--updated-from=")
		}
	}

	err := wails.Run(&options.App{
		Title:     "Khutaf",
		Width:     1120,
		Height:    760,
		MinWidth:  860,
		MinHeight: 560,
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 26, G: 26, B: 26, A: 1},
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
