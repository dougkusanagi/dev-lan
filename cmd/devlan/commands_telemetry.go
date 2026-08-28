package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dougkusanagi/dev-lan/internal/app"
)

type telemetryStatusOutput struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"`
	Queued   int    `json:"queued"`
}

func runTelemetry(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan telemetry status|enable ENDPOINT|disable|send")
	}
	store := service.Telemetry
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan telemetry status")
		}
		consent, err := store.Load()
		if err != nil {
			return err
		}
		queued, err := store.QueueSize()
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(telemetryStatusOutput{
			Enabled: consent.Enabled, Endpoint: consent.Endpoint, Queued: queued,
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	case "enable":
		if len(args) != 2 {
			return fmt.Errorf("uso: devlan telemetry enable ENDPOINT")
		}
		if err := store.SetConsent(true, args[1]); err != nil {
			return err
		}
		fmt.Println("Telemetria habilitada com consentimento explícito; o envio continua manual (`devlan telemetry send`).")
		return nil
	case "disable":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan telemetry disable")
		}
		if err := store.SetConsent(false, ""); err != nil {
			return err
		}
		fmt.Println("Telemetria desabilitada e fila local removida.")
		return nil
	case "send":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan telemetry send")
		}
		count, err := store.Send(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("%d evento(s) de telemetria enviado(s).\n", count)
		return nil
	default:
		return fmt.Errorf("subcomando telemetry desconhecido: %s", args[0])
	}
}
