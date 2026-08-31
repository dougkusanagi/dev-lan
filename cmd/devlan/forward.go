package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	localapi "github.com/dougkusanagi/dev-lan/internal/api"
	"github.com/dougkusanagi/dev-lan/internal/application"
)

// forwardableCommand is the subset already represented by the authenticated
// WSL command protocol. Keeping the allowlist explicit prevents a CLI command
// from silently changing semantics while the remaining families migrate.
func forwardableCommand(command string) bool {
	switch command {
	case "link", "unlink", "park", "unpark", "route":
		return true
	default:
		return false
	}
}

func forwardCommandIfActive(ctx context.Context, files application.EndpointFiles, command string, args []string) (bool, error) {
	if runtime.GOOS != "windows" || !forwardableCommand(command) {
		return false, nil
	}
	client := localapi.NewClientFromFiles(files)
	probeContext, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	probe, err := client.Do(probeContext, http.MethodGet, "/v1/health", nil)
	cancel()
	if err != nil {
		return false, nil
	}
	status := probe.StatusCode
	_ = probe.Body.Close()
	if status >= 400 {
		return true, fmt.Errorf("API local ativa, mas indisponível (HTTP %d)", status)
	}
	payload, err := client.Command(ctx, command, args)
	if err != nil {
		return true, err
	}
	printForwardedCommand(command, payload)
	return true, nil
}

func printForwardedCommand(command string, payload localapi.CommandResponse) {
	if payload.Message != nil && *payload.Message != "" {
		fmt.Println(*payload.Message)
	}
	if payload.Warnings != nil {
		printWarnings(*payload.Warnings)
	}
	if command == "route" {
		var data []byte
		switch {
		case payload.Allocations != nil:
			data, _ = json.MarshalIndent(*payload.Allocations, "", "  ")
		case payload.Paths != nil:
			data, _ = json.MarshalIndent(*payload.Paths, "", "  ")
		}
		if len(data) > 0 {
			fmt.Println(string(data))
		}
	}
}
