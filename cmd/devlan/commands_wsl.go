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
	if payload.Message != nil && *payload.Message != "" {
		fmt.Println(*payload.Message)
	}
	if command == "links" || command == "status" || command == "doctor" || command == "route" || command == "topology" {
		if command == "links" {
			if payload.Projects != nil {
				data, _ := json.MarshalIndent(*payload.Projects, "", "  ")
				fmt.Println(string(data))
			}
		} else if command == "status" {
			if payload.Status != nil {
				data, _ := json.MarshalIndent(*payload.Status, "", "  ")
				fmt.Println(string(data))
			}
		} else if command == "doctor" {
			if payload.Checks != nil {
				data, _ := json.MarshalIndent(*payload.Checks, "", "  ")
				fmt.Println(string(data))
			}
		} else if command == "topology" {
			data, _ := json.MarshalIndent(payload, "", "  ")
			fmt.Println(string(data))
		} else if payload.Allocations != nil {
			data, _ := json.MarshalIndent(*payload.Allocations, "", "  ")
			fmt.Println(string(data))
		} else if payload.Paths != nil {
			data, _ := json.MarshalIndent(*payload.Paths, "", "  ")
			fmt.Println(string(data))
		}
	}
	return nil
}
