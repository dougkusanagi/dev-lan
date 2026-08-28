package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	localapi "github.com/dougkusanagi/dev-lan/internal/api"
	"github.com/dougkusanagi/dev-lan/internal/app"
)

func runAPI(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 || args[0] == "serve" {
		server := localapi.New(service)
		endpoint, err := server.Start()
		if err != nil {
			return err
		}
		fmt.Printf("API local autenticada em %s\n", endpoint.Address)
		<-ctx.Done()
		return server.Close(context.Background())
	}
	if args[0] == "status" && len(args) == 1 {
		client := localapi.NewClient(service)
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
