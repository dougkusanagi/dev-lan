package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

func runAuth(ctx context.Context, service *app.App, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("uso: devlan auth enable default|NAME USERNAME PASSWORD | devlan auth disable default|NAME")
	}
	action := args[0]
	target := args[1]
	switch action {
	case "enable":
		if len(args) != 4 {
			return fmt.Errorf("uso: devlan auth enable default|NAME USERNAME PASSWORD")
		}
		res, err := service.SetAuth(ctx, target, true, args[2], args[3])
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Autenticação HTTP ativada para %s (usuário: %s).\n", target, args[2])
		return nil
	case "disable":
		res, err := service.DisableAuth(ctx, target)
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Autenticação HTTP desativada para %s.\n", target)
		return nil
	default:
		return fmt.Errorf("ação desconhecida: %s (use enable ou disable)", action)
	}
}

func runCA(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 || args[0] == "info" {
		info, err := service.CAInfo(ctx)
		if err != nil {
			return err
		}
		fmt.Println("=== Autoridade Certificadora (CA) DevLAN ===")
		fmt.Printf("Caminho do certificado raiz: %s\n", info["path"])
		fmt.Printf("Certificado existente: %s\n", info["exists"])
		if s, ok := info["size"]; ok {
			fmt.Printf("Tamanho: %s\n", s)
		}
		fmt.Println("\nPara instalar no Android/iOS/outro computador:")
		fmt.Println("1. Exporte o arquivo: devlan ca export")
		fmt.Println("2. Copie somente esse arquivo .crt para os outros dispositivos; a chave privada nunca é exportada.")
		return nil
	}
	switch args[0] {
	case "export":
		target := ""
		if len(args) > 1 {
			target = args[1]
		}
		savedPath, err := service.ExportCA(ctx, target)
		if err != nil {
			return err
		}
		fmt.Printf("Certificado raiz exportado com sucesso para: %s\n", savedPath)
		return nil
	case "rotate":
		res, err := service.RotateCA(ctx)
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Println("Rotação e recarregamento de certificados solicitados com sucesso.")
		return nil
	default:
		return fmt.Errorf("subcomando de ca desconhecido: %s (use info, export, rotate)", args[0])
	}
}

func runSecurity(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 || args[0] == "posture" {
		cfg, err := service.Config()
		if err != nil {
			return err
		}
		isPublic, netDetail, _ := platform.NetworkProfile(ctx)
		fmt.Println("=== Postura de Segurança DevLAN ===")
		if isPublic {
			fmt.Printf("[ALERTA] Perfil de rede: %s (recomenda-se modo Privado ou uso de Allowlist/Auth)\n", netDetail)
		} else {
			fmt.Println("[OK] Perfil de rede: Privada / confiável")
		}
		if len(cfg.Allowlist) > 0 {
			fmt.Printf("[OK] Allowlist global: %s\n", strings.Join(cfg.Allowlist, ", "))
		} else {
			fmt.Println("[INFO] Allowlist global: aberta na sub-rede")
		}
		if len(cfg.AuthUsers) > 0 {
			fmt.Printf("[OK] Basic Auth global: ativada (%d usuários)\n", len(cfg.AuthUsers))
		} else {
			fmt.Println("[INFO] Basic Auth global: desativada")
		}
		return nil
	}
	if args[0] == "audit" {
		lines := 50
		if len(args) >= 3 && args[1] == "--lines" {
			if l, err := strconv.Atoi(args[2]); err == nil && l > 0 {
				lines = l
			}
		}
		logs, err := service.SecurityAuditLogs(ctx, lines)
		if err != nil {
			return err
		}
		fmt.Print(logs)
		return nil
	}
	return fmt.Errorf("subcomando security desconhecido: %s (use posture, audit)", args[0])
}
