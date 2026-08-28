package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/application"
)

func runTopology(ctx context.Context, commands *application.Commands, queries *application.Queries, args []string) error {
	subcommand := "status"
	asJSON := false
	confirmed := false
	for _, arg := range args {
		switch arg {
		case "status", "check", "migrate", "repair":
			if subcommand != "status" {
				return fmt.Errorf("uso: devlan topology status|check|repair|migrate [--yes]")
			}
			subcommand = arg
		case "--json":
			asJSON = true
		case "--yes", "--confirm-wsl-shutdown":
			confirmed = true
		default:
			return fmt.Errorf("uso: devlan topology status|check|repair|migrate [--yes]")
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	switch subcommand {
	case "check":
		report := queries.Compatibility(ctx)
		if asJSON {
			return encoder.Encode(report)
		}
		fmt.Printf("Windows: %s (build %d)\n", report.WindowsVersion, report.WindowsBuild)
		fmt.Printf("WSL: %s | WSL2: %t | mirrored: %t | systemd: %t | loopback: %t | LAN: %t\n", report.WSLVersion, report.WSL2, report.MirroredNetworking, report.Systemd, report.LoopbackBidirectional, report.LANReachable)
		for _, check := range report.Checks {
			fmt.Printf("[%s] %-28s %s\n", check.Status, check.Name, check.Detail)
		}
		return nil
	case "repair":
		result, err := commands.RepairM8(ctx)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		if asJSON {
			return encoder.Encode(result)
		}
		fmt.Println("Topologia Caddy WSL único reconciliada (sem wsl --shutdown).")
		return nil
	case "migrate":
		if !confirmed {
			fmt.Fprintln(os.Stderr, "A migração reinicia o WSL inteiro e encerra todas as distribuições em execução.")
			fmt.Fprintln(os.Stderr, "Repita com `devlan topology migrate --yes` somente após salvar o trabalho em todas as distribuições.")
			return application.ErrWSLShutdownConfirmation
		}
		result, err := commands.MigrateTopology(ctx, true)
		if asJSON {
			_ = encoder.Encode(result)
		}
		if err != nil {
			return err
		}
		if !asJSON {
			fmt.Printf("Migração concluída: %s\nBackup: %s\nEtapas: %s\n", result.Topology, result.BackupDir, strings.Join(stringMigrationSteps(result.Steps), ", "))
		}
		return nil
	default:
		snapshot := queries.TopologyStatus(ctx)
		status := queries.CaddyStatus(ctx)
		if asJSON {
			return encoder.Encode(struct {
				Topology application.TopologyStatus `json:"topology"`
				Caddy    application.CaddyStatus    `json:"caddy"`
			}{Topology: snapshot, Caddy: status})
		}
		fmt.Printf("Topologia: %s\n", snapshot.Topology)
		fmt.Printf("Caddy WSL único: disponível=%t ativo=%t systemd=%t live=%t (%s)\n", status.Available, status.Running, status.Systemd, status.Live, status.Detail)
		if snapshot.WindowsConfig || snapshot.WSLConfig {
			fmt.Printf("Artefatos legados: Windows=%t WSL=%t; use `devlan topology migrate --yes`\n", snapshot.WindowsConfig, snapshot.WSLConfig)
		}
		return nil
	}
}

func stringMigrationSteps(steps []application.MigrationStep) []string {
	result := make([]string, 0, len(steps))
	for _, step := range steps {
		result = append(result, string(step))
	}
	return result
}
