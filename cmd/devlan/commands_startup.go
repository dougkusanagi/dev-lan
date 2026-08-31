package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/dougkusanagi/dev-lan/internal/startup"
)

func runStartup(ctx context.Context, dataDir string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan startup enable [gui|service] | disable | status")
	}
	switch args[0] {
	case "enable":
		if len(args) > 2 {
			return fmt.Errorf("uso: devlan startup enable [gui|service]")
		}
		// The browser-first control plane must run in the user's session. A
		// Windows SCM service defaults to LocalSystem and cannot see a WSL
		// distribution registered for the interactive user.
		mode := startup.ModeService
		if len(args) == 2 {
			parsed, err := startup.ParseMode(args[1])
			if err != nil {
				return err
			}
			mode = parsed
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		if err := startup.Enable(ctx, executable, dataDir, mode); err != nil {
			return err
		}
		fmt.Printf("Inicialização automática habilitada (%s).\n", mode)
		return nil
	case "disable":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan startup disable")
		}
		if err := startup.Disable(ctx); err != nil {
			return err
		}
		fmt.Println("Inicialização automática desabilitada.")
		return nil
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan startup status")
		}
		state, err := startup.Status(ctx)
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(state, "", "  ")
		fmt.Println(string(data))
		return nil
	default:
		return fmt.Errorf("subcomando startup desconhecido: %s", args[0])
	}
}
