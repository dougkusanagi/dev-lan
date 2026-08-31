package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"errors"

	localapi "github.com/dougkusanagi/dev-lan/internal/api"
	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/desktop"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

func runGUI(ctx context.Context, service *app.App, commands *application.Commands, queries *application.Queries, dataDir string, args []string) error {
	foreground := false
	for _, arg := range args {
		if arg == "--foreground" || arg == "-f" {
			foreground = true
		} else {
			return fmt.Errorf("uso: devlan gui [--foreground]")
		}
	}

	cfg, err := queries.Config(ctx)
	if err != nil {
		return err
	}
	uiPort := cfg.UIPort
	if uiPort == 0 {
		uiPort = 3210
	}

	if foreground {
		targetURL := "https://devlan.localhost/"
		if !queries.CaddyStatus(ctx).Live {
			targetURL = fmt.Sprintf("http://127.0.0.1:%d/", uiPort)
		}
		fmt.Printf("DevLAN GUI Web Server ativo em %s (porta %d)\n", targetURL, uiPort)
		fmt.Println("Pressione Ctrl+C para encerrar.")
		server := localapi.NewWithApplication(service, commands, queries)
		endpoint, err := server.Start()
		if err != nil && !errors.Is(err, localapi.ErrAlreadyRunning) {
			return err
		}
		_ = platform.OpenURL(targetURL)
		if endpoint.Address != "" {
			fmt.Printf("Servidor escutando em %s\n", endpoint.Address)
		}
		<-ctx.Done()
		return server.Close(context.Background())
	}

	// 1. Check if the server is already responsive
	client := localapi.NewClientFromFiles(queries.EndpointFiles())
	checkCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	res, checkErr := client.Do(checkCtx, http.MethodGet, "/v1/health", nil)
	cancel()
	isRunning := checkErr == nil && res != nil && res.StatusCode == http.StatusOK
	if res != nil {
		_ = res.Body.Close()
	}

	// 2. If not running, spawn detached background process
	if !isRunning {
		executable, execErr := os.Executable()
		if execErr != nil {
			return fmt.Errorf("obter caminho do executável: %w", execErr)
		}
		cmdArgs := []string{"api", "serve"}
		if dataDir != "" && dataDir != defaultDataDir() {
			cmdArgs = []string{"--data-dir", dataDir, "api", "serve"}
		}
		if err := platform.SpawnBackgroundDaemon(executable, cmdArgs); err != nil {
			return fmt.Errorf("iniciar servidor web em segundo plano: %w", err)
		}

		// Wait for server to become responsive
		started := false
		for i := 0; i < 30; i++ {
			time.Sleep(100 * time.Millisecond)
			waitCtx, waitCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			waitRes, waitErr := client.Do(waitCtx, http.MethodGet, "/v1/health", nil)
			waitCancel()
			if waitErr == nil && waitRes != nil && waitRes.StatusCode == http.StatusOK {
				_ = waitRes.Body.Close()
				started = true
				break
			}
			if waitRes != nil {
				_ = waitRes.Body.Close()
			}
		}
		if !started {
			return fmt.Errorf("servidor web em segundo plano não respondeu na porta %d", uiPort)
		}
	}

	// Re-check after starting the API. The API startup also performs a
	// best-effort reconciliation, which can restore the canonical Caddy edge
	// after a reboot. If recovery is unavailable, keep the direct loopback URL
	// as a reliable fallback instead of opening a known 502 origin.
	targetURL := fmt.Sprintf("http://127.0.0.1:%d/", uiPort)
	if queries.CaddyStatus(ctx).Live {
		targetURL = "https://devlan.localhost/"
	} else {
		reloadContext, reloadCancel := context.WithTimeout(ctx, 20*time.Second)
		if _, reloadErr := service.Reload(reloadContext); reloadErr == nil && queries.CaddyStatus(reloadContext).Live {
			targetURL = "https://devlan.localhost/"
		}
		reloadCancel()
	}

	fmt.Printf("Servidor Web DevLAN ativo em 127.0.0.1:%d\n", uiPort)

	if err := platform.OpenURL(targetURL); err != nil {
		fmt.Printf("Interface disponível em: %s (porta alternativa: http://127.0.0.1:%d/)\n", targetURL, uiPort)
	} else {
		fmt.Printf("Interface aberta no navegador: %s (porta alternativa: http://127.0.0.1:%d/)\n", targetURL, uiPort)
	}
	return nil
}

func runDesktop(ctx context.Context, dataDir string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan desktop install | status | uninstall")
	}
	switch args[0] {
	case "install":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan desktop install")
		}
		if err := desktop.Install(ctx, dataDir); err != nil {
			return err
		}
		fmt.Println("Integração desktop instalada com sucesso (atalhos criados).")
		return nil
	case "uninstall":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan desktop uninstall")
		}
		if err := desktop.Uninstall(ctx, dataDir); err != nil {
			return err
		}
		fmt.Println("Integração desktop removida.")
		return nil
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan desktop status")
		}
		st, err := desktop.CurrentState(ctx, dataDir)
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(data))
		return nil
	default:
		return fmt.Errorf("subcomando desktop desconhecido: %s (use install, status, uninstall)", args[0])
	}
}
