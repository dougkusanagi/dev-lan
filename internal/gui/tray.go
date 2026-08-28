package gui

import (
	"context"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func BuildAppMenu(ctx context.Context, guiApp *App) *menu.Menu {
	appMenu := menu.NewMenu()

	fileMenu := appMenu.AddSubmenu("DevLAN")
	fileMenu.AddText("Recarregar Serviços", keys.CmdOrCtrl("r"), func(_ *menu.CallbackData) {
		_ = guiApp.Reload()
		if guiApp.ctx != nil {
			wailsruntime.EventsEmit(guiApp.ctx, "devlan:reloaded")
		}
	})
	fileMenu.AddSeparator()
	fileMenu.AddText("Abrir Dashboard no Navegador", keys.CmdOrCtrl("o"), func(_ *menu.CallbackData) {
		status, err := guiApp.GetStatus()
		if err == nil {
			url := fmt.Sprintf("http://%s:%d", status.LANIP, status.WindowsPort)
			_ = guiApp.OpenURL(url)
		}
	})
	fileMenu.AddSeparator()
	fileMenu.AddText("Sair", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) {
		if guiApp.ctx != nil {
			wailsruntime.Quit(guiApp.ctx)
		} else {
			os.Exit(0)
		}
	})

	viewMenu := appMenu.AddSubmenu("Exibir")
	viewMenu.AddText("Atualizar Lista de Projetos", keys.Key("F5"), func(_ *menu.CallbackData) {
		if guiApp.ctx != nil {
			wailsruntime.EventsEmit(guiApp.ctx, "devlan:refresh")
		}
	})

	return appMenu
}

func BuildTrayMenu(ctx context.Context, guiApp *App) *menu.Menu {
	trayMenu := menu.NewMenu()

	status, err := guiApp.GetStatus()
	lanIP := status.LANIP
	if err != nil || lanIP == "" {
		lanIP = "127.0.0.1"
	}
	trayMenu.AddText(fmt.Sprintf("DevLAN (IP: %s)", lanIP), nil, func(_ *menu.CallbackData) {})
	trayMenu.AddSeparator()

	trayMenu.AddText("Abrir Painel", nil, func(_ *menu.CallbackData) {
		if guiApp.ctx != nil {
			wailsruntime.WindowShow(guiApp.ctx)
		}
	})

	trayMenu.AddText("Recarregar Caddy & Serviços", nil, func(_ *menu.CallbackData) {
		_ = guiApp.Reload()
		if guiApp.ctx != nil {
			wailsruntime.EventsEmit(guiApp.ctx, "devlan:reloaded")
		}
	})

	trayMenu.AddSeparator()
	trayMenu.AddText("Sair do DevLAN", nil, func(_ *menu.CallbackData) {
		if guiApp.ctx != nil {
			wailsruntime.Quit(guiApp.ctx)
		} else {
			os.Exit(0)
		}
	})

	return trayMenu
}
