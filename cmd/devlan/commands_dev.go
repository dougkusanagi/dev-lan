package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/dougkusanagi/dev-lan/internal/app"
)

func runDevCommand(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan dev NAME [start|stop|restart|status|logs|--port PORT|--command CMD|--pm PM]")
	}
	name := args[0]
	if len(args) == 1 {
		return service.StartDev(ctx, name)
	}
	switch args[1] {
	case "start":
		return service.StartDev(ctx, name)
	case "stop":
		return service.StopDev(ctx, name)
	case "restart":
		return service.RestartDev(ctx, name)
	case "status":
		st, err := service.DevStatus(ctx, name)
		if err != nil {
			return err
		}
		fmt.Printf("Projeto: %s | Porta: %d | Estado: %s | PID: %d\n", st.ProjectName, st.Port, st.State, st.PID)
		return nil
	case "logs":
		logs, err := service.ProjectDevLogs(ctx, name, 100)
		if err != nil {
			return err
		}
		fmt.Print(logs)
		return nil
	default:
		// parse flags
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--port":
				if i+1 >= len(args) {
					return fmt.Errorf("--port exige um número de porta")
				}
				port, err := strconv.Atoi(args[i+1])
				if err != nil || port < 1024 || port > 65535 {
					return fmt.Errorf("porta dev inválida: %s", args[i+1])
				}
				res, err := service.SetProjectDevPort(ctx, name, port)
				printWarnings(res.Warnings)
				if err != nil {
					return err
				}
				i++
			case "--command", "--cmd":
				if i+1 >= len(args) {
					return fmt.Errorf("--command exige uma string de comando")
				}
				res, err := service.SetProjectDevCommand(ctx, name, args[i+1])
				printWarnings(res.Warnings)
				if err != nil {
					return err
				}
				i++
			case "--pm":
				if i+1 >= len(args) {
					return fmt.Errorf("--pm exige um gerenciador (npm, pnpm, yarn, bun)")
				}
				res, err := service.SetProjectPackageManager(ctx, name, args[i+1])
				printWarnings(res.Warnings)
				if err != nil {
					return err
				}
				i++
			default:
				return fmt.Errorf("opção desconhecida: %s", args[i])
			}
		}
		fmt.Printf("Configurações de dev do projeto %s atualizadas.\n", name)
		return nil
	}
}
