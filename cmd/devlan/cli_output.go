package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

func printProjects(ctx context.Context, queries *application.Queries, filter string, asJSON bool) error {
	cfg, err := queries.Config(ctx)
	if err != nil {
		return err
	}
	effective, err := queries.EffectiveConfig(ctx, cfg)
	if err != nil {
		return err
	}
	host := queries.LANAddress()
	if len(effective.Projects) == 0 {
		if asJSON {
			fmt.Println("[]")
			return nil
		}
		fmt.Println("Nenhum projeto registrado.")
		return nil
	}
	rows := make([]projectRow, 0, len(effective.Projects))
	filterLower := strings.ToLower(strings.TrimSpace(filter))
	for _, project := range effective.Projects {
		if filterLower != "" && !strings.Contains(strings.ToLower(project.Name), filterLower) && !strings.Contains(strings.ToLower(project.Path), filterLower) {
			continue
		}
		resolved, err := effective.Resolve(project.Name)
		if err != nil {
			return err
		}
		localURL := domain.LocalDevURL(project.Name)
		lanURL := resolved.URL(host, cfg.WindowsPort, cfg.HTTPSPort, effective.SecureProject(project))

		runtimeStr := "-"
		typeStr := string(resolved.Mode)
		switch resolved.Mode {
		case domain.ModePHP:
			runtimeStr = effective.EffectivePHPVersion(project)
			typeStr = string(effective.PHPProjectPreset(project))
		case domain.ModeStatic:
			staticDir := "root"
			if project.StaticDir != nil && *project.StaticDir != "" {
				staticDir = *project.StaticDir
			}
			runtimeStr = "-"
			typeStr = "static (" + staticDir + ")"
		case domain.ModeDev:
			fw := "generic"
			if project.DevFramework != nil {
				fw = *project.DevFramework
			}
			port := effective.DevPort(project)
			runtimeStr = fmt.Sprintf("%s :%d", fw, port)
			pm := effective.PackageManager(project)
			typeStr = "dev (" + pm + ")"
		}

		rows = append(rows, projectRow{
			Name:     project.Name,
			Mode:     string(resolved.Mode),
			Runtime:  runtimeStr,
			Type:     typeStr,
			Source:   string(resolved.Source),
			SSL:      sslState(cfg.SecureProject(project)),
			URL:      lanURL,
			LocalURL: localURL,
			LANURL:   lanURL,
			Path:     project.Path,
		})
	}
	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(rows)
	}
	if len(rows) == 0 {
		fmt.Printf("Nenhum projeto encontrado para o filtro %q.\n", filter)
		return nil
	}
	if err := writeProjectTable(os.Stdout, rows); err != nil {
		return err
	}
	fmt.Printf("\n💡 Dica: execute `devlan open %s` para abrir no navegador local. Na LAN, cookies HTTP não são isolados por porta.\n", rows[0].Name)
	return nil
}

type projectRow struct {
	Name     string `json:"name"`
	Mode     string `json:"mode"`
	Runtime  string `json:"runtime"`
	Type     string `json:"type"`
	Source   string `json:"source"`
	SSL      string `json:"ssl"`
	URL      string `json:"url"`
	LocalURL string `json:"local_url"`
	LANURL   string `json:"lan_url"`
	Path     string `json:"path"`
}

func writeProjectTable(output io.Writer, rows []projectRow) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "PROJETO\tMODO\tRUNTIME\tTIPO\tSSL\tURL LOCAL\tURL LAN\tCAMINHO"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "-------\t----\t-------\t----\t---\t---------\t-------\t-------"); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.Name, row.Mode, row.Runtime, row.Type, row.SSL, row.LocalURL, row.LANURL, row.Path); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func sslState(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func printStatus(ctx context.Context, queries *application.Queries, dataDir string) error {
	cfg, err := queries.Config(ctx)
	if err != nil {
		return err
	}
	if current, generated, diverged := queries.CheckLANAddressDivergence(); diverged {
		fmt.Fprintf(os.Stderr, "[aviso] O IP da rede local mudou de %s para %s. Execute `devlan reload` para atualizar o Caddy.\n", generated, current)
	}
	fmt.Printf("DevLAN %s (%s)\n", version, application.RuntimeDescription())
	fmt.Printf("Dados: %s\n", dataDir)
	fmt.Printf("Padrão: %s | LAN HTTP: %d | LAN HTTPS: %d | SSL: %s | pool: %d-%d\n", cfg.DefaultMode, cfg.WindowsPort, cfg.HTTPSPort, sslState(cfg.TLSEnabled), cfg.RouteBasePort, cfg.RouteBasePort+cfg.RoutePortCount-1)
	caddyStatus := queries.CaddyStatus(ctx)
	topology := queries.TopologyStatus(ctx)
	fmt.Printf("Caddy WSL único: disponível=%t ativo=%t systemd=%t live=%t | topologia=%s\n", caddyStatus.Available, caddyStatus.Running, caddyStatus.Systemd, caddyStatus.Live, topology.Topology)
	if versions, versionErr := queries.PHPVersions(ctx); versionErr == nil {
		labels := make([]string, 0, len(versions))
		for _, version := range versions {
			status := "detectada"
			if version.Configured {
				status = "configurada"
			}
			if version.Configured && version.Installed {
				status = "ativa"
			}
			labels = append(labels, version.Version+" ("+status+")")
		}
		fmt.Printf("PHP padrão: %s | versões: %s\n", cfg.PHPDefaultVersion, strings.Join(labels, ", "))
	} else {
		fmt.Printf("PHP: indisponível (%s)\n", versionErr)
	}
	fmt.Printf("Projetos registrados: %d | parks: %d\n", len(cfg.Projects), len(cfg.Parks))
	return printProjects(ctx, queries, "", false)
}

func printWarnings(warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "[aviso] %s\n", warning)
	}
}

func printUninstallPlan(plan application.UninstallPlan, dryRun bool) {
	if dryRun {
		fmt.Println("Plano de desinstalação (nenhuma alteração foi feita):")
	} else {
		fmt.Println("Resultado da desinstalação:")
	}
	counts := map[application.UninstallAction]int{}
	for _, item := range plan.Items {
		counts[item.Action]++
		fmt.Printf("  %-9s %-24s %s\n", item.Action, item.ID, item.Detail)
	}
	fmt.Printf("Resumo: remover=%d restaurar=%d preservar=%d conflito=%d pendente=%d falha=%d\n",
		counts[application.UninstallRemove], counts[application.UninstallRestore], counts[application.UninstallPreserve], counts[application.UninstallConflict], counts[application.UninstallPending], counts[application.UninstallFailed])
	if plan.ProjectCount > 0 {
		fmt.Printf("Projetos preservados: %d\n", plan.ProjectCount)
	}
}
