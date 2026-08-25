package gui

import (
	"context"

	"github.com/dougkusanagi/dev-lan/frontend"
	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

func Launch(service *app.App) error {
	guiApp := NewApp(service)

	return wails.Run(&options.App{
		Title:             "DevLAN Dashboard",
		Width:             1100,
		Height:            740,
		MinWidth:          840,
		MinHeight:         580,
		DisableResize:     false,
		Fullscreen:        false,
		Frameless:         false,
		StartHidden:       false,
		HideWindowOnClose: false,
		BackgroundColour:  &options.RGBA{R: 15, G: 23, B: 42, A: 255},
		AssetServer: &assetserver.Options{
			Assets: frontend.Assets,
		},
		Menu: BuildAppMenu(context.Background(), guiApp),
		OnStartup: func(ctx context.Context) {
			guiApp.Startup(ctx)
		},
		Bind: []interface{}{
			guiApp,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			BackdropType:         windows.Mica,
			Theme:                windows.SystemDefault,
		},
	})
}
