package main

import (
	"context"
	"encoding/json"
	"fmt"

	backgroundservice "github.com/dougkusanagi/dev-lan/internal/service"
)

func runBackgroundService(ctx context.Context, dataDir string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan service install|remove|start|stop|status|run")
	}
	manager := backgroundservice.NewManager()
	switch args[0] {
	case "run":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan service run")
		}
		return backgroundservice.Run(ctx, dataDir)
	case "install":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan service install")
		}
		options, err := backgroundservice.DefaultOptions(dataDir)
		if err != nil {
			return err
		}
		if err := manager.Install(ctx, options); err != nil {
			return err
		}
		fmt.Println("Serviço DevLAN instalado para iniciar automaticamente no boot.")
		return nil
	case "remove":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan service remove")
		}
		if err := manager.Remove(ctx); err != nil {
			return err
		}
		fmt.Println("Serviço DevLAN removido.")
		return nil
	case "start", "stop":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan service %s", args[0])
		}
		var err error
		if args[0] == "start" {
			err = manager.Start(ctx)
		} else {
			err = manager.Stop(ctx)
		}
		if err != nil {
			return err
		}
		fmt.Printf("Serviço DevLAN: %s.\n", args[0])
		return nil
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan service status")
		}
		status, err := manager.Status(ctx)
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(status, "", "  ")
		fmt.Println(string(data))
		return nil
	default:
		return fmt.Errorf("subcomando service desconhecido: %s", args[0])
	}
}
