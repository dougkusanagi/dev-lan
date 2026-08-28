package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	localapi "github.com/dougkusanagi/dev-lan/internal/api"
)

func runWSLClient(ctx context.Context, dataDir, command string, args []string) error {
	allowed := map[string]bool{
		"link": true, "unlink": true, "park": true, "unpark": true,
		"links": true, "status": true, "reload": true, "doctor": true, "open": true, "route": true,
		"topology": true,
	}
	if !allowed[command] {
		return fmt.Errorf("comando %q ainda não está disponível no cliente WSL; use o controlador Windows", command)
	}
	client := localapi.NewClientForDataDir(dataDir)
	requestContext, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	payload, err := client.Command(requestContext, command, args)
	if err != nil {
		return fmt.Errorf("controlador Windows indisponível: %w (inicie `devlan service start` ou a UI)", err)
	}
	if message, ok := payload["message"].(string); ok && message != "" {
		fmt.Println(message)
	}
	if command == "links" || command == "status" || command == "doctor" || command == "route" || command == "topology" {
		if command == "links" {
			if projects, ok := payload["projects"]; ok {
				data, _ := json.MarshalIndent(projects, "", "  ")
				fmt.Println(string(data))
			}
		} else if command == "status" {
			if status, ok := payload["status"]; ok {
				data, _ := json.MarshalIndent(status, "", "  ")
				fmt.Println(string(data))
			}
		} else if command == "doctor" {
			if checks, ok := payload["checks"]; ok {
				data, _ := json.MarshalIndent(checks, "", "  ")
				fmt.Println(string(data))
			}
		} else if command == "topology" {
			data, _ := json.MarshalIndent(payload, "", "  ")
			fmt.Println(string(data))
		} else if allocations, ok := payload["allocations"]; ok {
			data, _ := json.MarshalIndent(allocations, "", "  ")
			fmt.Println(string(data))
		} else if paths, ok := payload["paths"]; ok {
			data, _ := json.MarshalIndent(paths, "", "  ")
			fmt.Println(string(data))
		}
	}
	return nil
}
