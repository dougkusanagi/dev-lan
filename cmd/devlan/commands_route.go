package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

func runRoute(ctx context.Context, service *app.App, queries *application.Queries, args []string) error {
	if len(args) > 0 && args[0] == "allocations" {
		return runRouteAllocations(ctx, service, args[1:])
	}
	if len(args) == 0 {
		cfg, err := queries.Config(ctx)
		if err != nil {
			return err
		}
		eff, err := queries.EffectiveConfig(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Printf("Portas LAN: automáticas a partir de %d\n\n", cfg.RouteBasePort)
		writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "PROJETO\tPORTA LAN\tURL LOCAL\tURL LAN\tOVERRIDE")
		for _, p := range eff.Projects {
			port := eff.EffectiveRoutePort(p)
			override := "auto"
			if p.RoutePort != nil {
				override = "customizada"
			}
			fmt.Fprintf(writer, "%s\t%d\t%s\t%s\t%s\n", p.Name, port, domain.LocalDevURL(p.Name), routeURL(cfg, p, port), override)
		}
		if err := writer.Flush(); err != nil {
			return err
		}
		fmt.Println("\n💡 Nota: Na rede local (LAN), cookies HTTP não são isolados por porta no mesmo IP.")
		return nil
	}
	name := args[0]
	if len(args) == 1 {
		cfg, err := queries.Config(ctx)
		if err != nil {
			return err
		}
		eff, err := queries.EffectiveConfig(ctx, cfg)
		if err != nil {
			return err
		}
		p, found := eff.Project(name)
		if !found {
			return fmt.Errorf("projeto não encontrado: %s", name)
		}
		override := "automática"
		if p.RoutePort != nil {
			override = "customizada"
		}
		fmt.Printf("Projeto %s: porta LAN %d (%s)\n", name, eff.EffectiveRoutePort(p), override)
		fmt.Printf("Local: %s\nLAN: %s\n", domain.LocalDevURL(p.Name), routeURL(cfg, p, eff.EffectiveRoutePort(p)))
		fmt.Println("\n💡 Nota: Na rede local (LAN), cookies HTTP não são isolados por porta no mesmo IP.")
		return nil
	}
	if len(args) != 3 || args[1] != "--port" {
		return fmt.Errorf("uso: devlan route NAME --port auto|PORT")
	}
	var port *int
	if args[2] != "auto" {
		parsed, err := strconv.Atoi(args[2])
		if err != nil || parsed < 1024 || parsed > 65535 {
			return fmt.Errorf("porta inválida: %s", args[2])
		}
		port = &parsed
	}
	res, err := service.SetRoutePort(ctx, name, port)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}
	fmt.Printf("Porta LAN do projeto %s atualizada.\n", name)
	return nil
}

func runRouteAllocations(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 {
		allocations, err := service.RouteAllocations(ctx)
		if err != nil {
			return err
		}
		if len(allocations) == 0 {
			fmt.Println("Nenhuma alocação automática persistida.")
			return nil
		}
		writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "CAMINHO\tPORTA\tSTATUS")
		for _, allocation := range allocations {
			status := "ativa"
			if allocation.Orphan {
				status = "órfã (prune explícito)"
			}
			fmt.Fprintf(writer, "%s\t%d\t%s\n", allocation.Path, allocation.Port, status)
		}
		return writer.Flush()
	}
	if args[0] != "prune" {
		return fmt.Errorf("uso: devlan route allocations | devlan route allocations prune [--dry-run]")
	}
	dryRun := false
	if len(args) == 2 && args[1] == "--dry-run" {
		dryRun = true
	} else if len(args) != 1 {
		return fmt.Errorf("uso: devlan route allocations prune [--dry-run]")
	}
	paths, result, err := service.PruneRouteAllocations(ctx, dryRun)
	printWarnings(result.Warnings)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		fmt.Println("Nenhuma alocação órfã encontrada.")
		return nil
	}
	if dryRun {
		fmt.Printf("%d alocação(ões) seriam removidas:\n", len(paths))
	} else {
		fmt.Printf("%d alocação(ões) órfãs removidas:\n", len(paths))
	}
	for _, path := range paths {
		fmt.Printf("- %s\n", path)
	}
	return nil
}

func routeURL(cfg domain.Config, project domain.Project, port int) string {
	host := cfg.LANAddress
	if host == "" || host == "auto" {
		host, _ = platform.LANAddress()
		if host == "" {
			host = "localhost"
		}
	}
	resolved := domain.ResolvedProject{Project: project, RoutePort: port}
	return resolved.URL(host, cfg.WindowsPort, cfg.HTTPSPort, cfg.SecureProject(project))
}

func runExpose(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan expose NAME [--duration 30m|1h|2h]")
	}
	name := args[0]
	var duration time.Duration
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--duration" && i+1 < len(args) {
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return fmt.Errorf("duração inválida: %s", args[i])
			}
			duration = d
		} else {
			return fmt.Errorf("opção desconhecida %s", arg)
		}
	}
	res, projName, err := service.ExposeProject(ctx, name, duration)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}
	if duration > 0 {
		fmt.Printf("Projeto %s exposto temporariamente por %v.\n", projName, duration)
	} else {
		fmt.Printf("Projeto %s exposto.\n", projName)
	}
	return nil
}

func runUnexpose(ctx context.Context, service *app.App, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: devlan unexpose NAME")
	}
	res, projName, err := service.UnexposeProject(ctx, args[0])
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}
	fmt.Printf("Exposição do projeto %s revogada.\n", projName)
	return nil
}

func runAllowlist(ctx context.Context, service *app.App, queries *application.Queries, args []string) error {
	if len(args) == 0 {
		cfg, err := queries.Config(ctx)
		if err != nil {
			return err
		}
		eff, err := queries.EffectiveConfig(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Printf("Allowlist Global: %v\n\n", cfg.Allowlist)
		writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "PROJETO\tALLOWLIST\tORIGEM")
		for _, p := range eff.Projects {
			origin := "herdada (global)"
			if len(p.Allowlist) > 0 {
				origin = "específica do projeto"
			}
			al := eff.EffectiveAllowlist(p)
			fmt.Fprintf(writer, "%s\t%s\t%s\n", p.Name, strings.Join(al, ", "), origin)
		}
		return writer.Flush()
	}
	sub := args[0]
	switch sub {
	case "set":
		if len(args) < 3 {
			return fmt.Errorf("uso: devlan allowlist set default|NAME CIDR...")
		}
		res, err := service.SetAllowlist(ctx, args[1], args[2:])
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Allowlist de %s atualizada.\n", args[1])
		return nil
	case "add":
		if len(args) < 3 {
			return fmt.Errorf("uso: devlan allowlist add default|NAME CIDR...")
		}
		res, err := service.AddAllowlist(ctx, args[1], args[2:])
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("IPs/CIDRs adicionados à allowlist de %s.\n", args[1])
		return nil
	case "remove":
		if len(args) < 3 {
			return fmt.Errorf("uso: devlan allowlist remove default|NAME CIDR...")
		}
		res, err := service.RemoveAllowlist(ctx, args[1], args[2:])
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("IPs/CIDRs removidos da allowlist de %s.\n", args[1])
		return nil
	case "clear":
		if len(args) != 2 {
			return fmt.Errorf("uso: devlan allowlist clear default|NAME")
		}
		res, err := service.ClearAllowlist(ctx, args[1])
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Allowlist de %s limpa.\n", args[1])
		return nil
	default:
		cfg, err := queries.Config(ctx)
		if err != nil {
			return err
		}
		eff, err := queries.EffectiveConfig(ctx, cfg)
		if err != nil {
			return err
		}
		p, found := eff.Project(args[0])
		if !found {
			return fmt.Errorf("projeto não encontrado: %s", args[0])
		}
		fmt.Printf("Allowlist efetiva de %s: %s\n", p.Name, strings.Join(eff.EffectiveAllowlist(p), ", "))
		return nil
	}
}
