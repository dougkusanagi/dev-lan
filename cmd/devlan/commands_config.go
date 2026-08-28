package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/app"
)

func runConfig(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan config export [PATH] | devlan config import PATH")
	}
	switch args[0] {
	case "export":
		if len(args) > 2 {
			return fmt.Errorf("uso: devlan config export [PATH]")
		}
		data, err := service.ExportConfig()
		if err != nil {
			return err
		}
		if len(args) == 1 || args[1] == "-" {
			_, err = os.Stdout.Write(data)
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Clean(args[1])), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(args[1], data, 0o600); err != nil {
			return fmt.Errorf("gravar exportação: %w", err)
		}
		fmt.Printf("Configuração exportada para %s (sem credenciais)\n", args[1])
		return nil
	case "import":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" || args[1] == "-" {
			return fmt.Errorf("uso: devlan config import PATH")
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			return fmt.Errorf("ler importação: %w", err)
		}
		result, err := service.ImportConfig(ctx, data)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Println("Configuração importada, validada e aplicada.")
		return nil
	default:
		return fmt.Errorf("subcomando config desconhecido: %s", args[0])
	}
}
