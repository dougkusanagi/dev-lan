package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	localapi "github.com/dougkusanagi/dev-lan/internal/api"
	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/application"
)

func runAPI(ctx context.Context, service *app.App, commands *application.Commands, queries *application.Queries, args []string) error {
	if len(args) == 0 || args[0] == "serve" {
		server := localapi.NewWithApplication(service, commands, queries)
		endpoint, err := server.Start()
		if err != nil {
			return err
		}
		fmt.Printf("API local autenticada em %s\n", endpoint.Address)
		// The browser reaches the dashboard through the canonical Caddy origin.
		// Starting only the API leaves an existing Caddy process proxying to a
		// dead upstream after reboot or an interrupted user-session launch.
		// Reconcile asynchronously so health/status remain available even when
		// WSL or Caddy needs recovery.
		startupContext, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		go func() {
			if _, reloadErr := service.Reload(startupContext); reloadErr != nil {
				service.Audit("API_START_RELOAD_FAILED", reloadErr.Error())
			}
		}()
		<-ctx.Done()
		cancel()
		return server.Close(context.Background())
	}
	if args[0] == "status" && len(args) == 1 {
		client := localapi.NewClientFromFiles(queries.EndpointFiles())
		response, err := client.Do(ctx, http.MethodGet, "/v1/health", nil)
		if err != nil {
			return fmt.Errorf("API local indisponível: %w", err)
		}
		defer response.Body.Close()
		data, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			return readErr
		}
		fmt.Print(string(data))
		if response.StatusCode >= 400 {
			return fmt.Errorf("API local respondeu HTTP %d", response.StatusCode)
		}
		return nil
	}
	return fmt.Errorf("uso: devlan api serve | devlan api status")
}
