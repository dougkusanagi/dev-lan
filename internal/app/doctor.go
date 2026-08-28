package app

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/domain"
	phpconfig "github.com/dougkusanagi/dev-lan/internal/php"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a

func (a *App) Doctor(ctx context.Context, projectName string) ([]Check, error) {
	ctx = platform.WithWSLOperation(ctx, platform.WSLOperationDoctor)
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	checks := []Check{}
	if cfg.LANAddress == "auto" {
		if address, err := a.resourceUseCases().LANAddress(ctx, cfg.LANAddress); err != nil {
			checks = append(checks, Check{"IP LAN", "WARN", err.Error()})
		} else {
			generated := extractCaddyLANAddress(a.Store.Paths().Caddy)
			if generated == "" {
				// Read-only compatibility with a pre-M8 generated edge.
				generated = extractCaddyLANAddress(a.Store.Paths().WindowsCaddy)
			}
			if generated != "" && generated != "localhost" && generated != "127.0.0.1" && address != generated {
				checks = append(checks, Check{"IP LAN", "WARN", fmt.Sprintf("IP atual (%s) diverge do Caddyfile (%s); execute `devlan reload`", address, generated)})
			} else {
				checks = append(checks, Check{"IP LAN", "OK", address})
			}
		}
	} else {
		checks = append(checks, Check{"IP LAN", "OK", cfg.LANAddress + " (configurado)"})
	}

	caddyClient := a.edgeCaddy()
	if err := a.resourceUseCases().CaddyAvailable(ctx); err != nil {
		checks = append(checks, Check{"Caddy WSL único", "WARN", "não encontrado: " + err.Error()})
	} else {
		status := a.edgeCaddy().Status(ctx)
		if !status.Running && !status.Live {
			checks = append(checks, Check{"Caddy WSL único", "WARN", "binário disponível, mas serviço/live indisponível"})
		} else {
			detail := "serviço Caddy WSL ativo"
			if status.Systemd {
				detail = "serviço systemd do Caddy WSL ativo"
			}
			checks = append(checks, Check{"Caddy WSL único", "OK", detail})
		}
	}

	compatibility := a.WSLCompatibility(ctx)
	for _, item := range compatibility.Checks {
		checks = append(checks, Check{"WSL " + item.Name, string(item.Status), item.Detail})
	}

	// Check Node & JS package managers in one WSL session.
	tools := []string{"node", "npm", "pnpm", "yarn", "bun"}
	if found, findErr := a.WSL.HasCommands(ctx, tools...); findErr == nil {
		for _, tool := range tools {
			if found[tool] {
				checks = append(checks, Check{"WSL " + tool, "OK", "disponível"})
			}
		}
	}

	caddyStatus := caddyClient.Status(ctx)
	adminRunning := caddyStatus.Running || caddyStatus.Live
	if adminRunning {
		checks = append(checks, Check{"Porta HTTP (80)", "OK", "gerenciada diretamente pelo Caddy WSL único"})
		if cfg.TLSEnabled {
			checks = append(checks, Check{"Porta HTTPS (443)", "OK", "gerenciada diretamente pelo Caddy WSL único"})
		}
	} else {
		if platform.IsPortAvailable(80) {
			checks = append(checks, Check{"Porta HTTP (80)", "OK", "disponível"})
		} else {
			checks = append(checks, Check{"Porta HTTP (80)", "WARN", "ocupada por outro processo; possível conflito"})
		}
		if cfg.TLSEnabled {
			if platform.IsPortAvailable(443) {
				checks = append(checks, Check{"Porta HTTPS (443)", "OK", "disponível"})
			} else {
				checks = append(checks, Check{"Porta HTTPS (443)", "WARN", "ocupada por outro processo; possível conflito"})
			}
		}
	}

	if len(cfg.PHPVersions) == 0 {
		phpCommands := []string{"php-fpm", "php-fpm8.5", "php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.1"}
		phpFound := false
		if found, findErr := a.WSL.HasCommands(ctx, phpCommands...); findErr == nil {
			for _, command := range phpCommands {
				if found[command] {
					checks = append(checks, Check{"PHP-FPM", "OK", command})
					phpFound = true
					break
				}
			}
		}
		if !phpFound {
			checks = append(checks, Check{"PHP-FPM", "WARN", "nenhum executável suportado encontrado no WSL"})
		}
		if socket, err := a.WSL.IsSocket(ctx, cfg.PHPFPMOsocket); err != nil {
			checks = append(checks, Check{"Socket PHP-FPM", "WARN", "WSL indisponível"})
		} else if socket {
			checks = append(checks, Check{"Socket PHP-FPM", "OK", cfg.PHPFPMOsocket})
		} else {
			checks = append(checks, Check{"Socket PHP-FPM", "WARN", cfg.PHPFPMOsocket + " não é socket"})
		}
	} else {
		installed := map[string]platform.PHPInstallation{}
		if a.PHP != nil {
			items, listErr := a.PHP.List(ctx)
			if listErr == nil {
				for _, item := range items {
					installed[item.Version] = item
				}
			}
		}
		socketPaths := make([]string, 0, len(cfg.PHPVersions))
		for _, version := range cfg.PHPVersions {
			socketPaths = append(socketPaths, domain.PHPSharedSocket(version.Version))
		}
		sockets, socketErr := a.WSL.IsSockets(ctx, socketPaths...)
		for index, version := range cfg.PHPVersions {
			if item, found := installed[version.Version]; found {
				checks = append(checks, Check{"PHP " + version.Version, "OK", item.FPMBinary})
			} else {
				checks = append(checks, Check{"PHP " + version.Version, "WARN", "versão registrada, mas executável não foi encontrado"})
			}
			pool := cfg.PHPFPMPool
			if !version.Pool.IsZero() {
				pool = version.Pool
			}
			checks = append(checks, Check{"Pool PHP " + version.Version, "OK", fmt.Sprintf("ondemand, max_children=%d, idle_timeout=%s, max_requests=%d", pool.MaxChildren, pool.IdleTimeout, pool.MaxRequests)})
			socketPath := socketPaths[index]
			if socketErr != nil {
				checks = append(checks, Check{"Socket PHP " + version.Version, "WARN", "WSL indisponível"})
			} else if sockets[socketPath] {
				checks = append(checks, Check{"Socket PHP " + version.Version, "OK", socketPath})
			} else {
				checks = append(checks, Check{"Socket PHP " + version.Version, "WARN", socketPath + " não é socket"})
			}
		}
	}

	if networkProfile, profileErr := a.resourceUseCases().NetworkProfile(ctx); profileErr == nil && networkProfile.Public {
		checks = append(checks, Check{"Rede Pública", "WARN", networkProfile.Detail})
	} else {
		checks = append(checks, Check{"Perfil de Rede", "OK", "Privada / confiável"})
	}

	if len(cfg.Allowlist) > 0 {
		checks = append(checks, Check{"Allowlist Global", "OK", strings.Join(cfg.Allowlist, ", ")})
	} else {
		checks = append(checks, Check{"Allowlist Global", "OK", "aberto para sub-rede privada"})
	}

	if caInfo, err := a.CAInfo(ctx); err == nil && caInfo["exists"] == "true" {
		if runtime.GOOS != "windows" {
			checks = append(checks, Check{"CA Local", "WARN", fmt.Sprintf("certificado raiz presente (%s), mas a confiança só é verificada no Windows", caInfo["path"])})
		} else if trusted, trustErr := a.resourceUseCases().IsTrusted(ctx, caInfo["path"]); trustErr != nil {
			checks = append(checks, Check{"CA Local", "WARN", "não foi possível verificar a confiança da CA: " + trustErr.Error()})
		} else if !trusted {
			checks = append(checks, Check{"CA Local", "WARN", "certificado raiz presente, mas não confiado; execute `devlan trust` como Administrador"})
		} else {
			checks = append(checks, Check{"CA Local", "OK", fmt.Sprintf("certificado raiz presente e confiado (%s)", caInfo["path"])})
		}
	} else {
		checks = append(checks, Check{"CA Local", "WARN", "certificado raiz não encontrado; execute `devlan trust` como Administrador"})
	}

	firewallSpec := platform.FirewallSpecForConfig(cfg)
	if healthy, inspectErr := a.FirewallHealthy(ctx, cfg); inspectErr != nil {
		if errors.Is(inspectErr, platform.ErrFirewallNotFound) {
			checks = append(checks, Check{"Firewall", "WARN", "regra DevLAN ausente; execute `devlan install` ou `devlan reload` como Administrador"})
		} else {
			checks = append(checks, Check{"Firewall", "WARN", "regra DevLAN não confirmada: " + inspectErr.Error()})
		}
	} else if !healthy {
		checks = append(checks, Check{"Firewall", "FAIL", "regra DevLAN divergente (direção, ação, protocolo, portas, perfil ou origem); execute `devlan reload` como Administrador"})
	} else {
		checks = append(checks, Check{"Firewall", "OK", "regra DevLAN reconciliada: TCP " + firewallSpecDescription(firewallSpec)})
	}
	if composite, ok := a.Firewall.(platform.CompositeFirewall); ok {
		hyperVStatus := composite.HyperVStatus(ctx, firewallSpec)
		if !hyperVStatus.Supported {
			checks = append(checks, Check{"Hyper-V Firewall", "WARN", hyperVStatus.Detail})
		} else if hyperVStatus.Healthy {
			checks = append(checks, Check{"Hyper-V Firewall", "OK", "Private / LocalSubnet, default inbound Block"})
		} else {
			checks = append(checks, Check{"Hyper-V Firewall", "WARN", hyperVStatus.Detail})
		}
	} else if composite, ok := a.Firewall.(*platform.CompositeFirewall); ok && composite != nil {
		hyperVStatus := composite.HyperVStatus(ctx, firewallSpec)
		if !hyperVStatus.Supported {
			checks = append(checks, Check{"Hyper-V Firewall", "WARN", hyperVStatus.Detail})
		} else if hyperVStatus.Healthy {
			checks = append(checks, Check{"Hyper-V Firewall", "OK", "Private / LocalSubnet, default inbound Block"})
		} else {
			checks = append(checks, Check{"Hyper-V Firewall", "WARN", hyperVStatus.Detail})
		}
	}

	uiPort := cfg.UIPort
	if uiPort == 0 {
		uiPort = 3210
	}
	if platform.IsPortAvailable(uiPort) {
		checks = append(checks, Check{fmt.Sprintf("Porta Web/API (%d)", uiPort), "OK", "disponível para servidor loopback"})
	} else {
		checks = append(checks, Check{fmt.Sprintf("Porta Web/API (%d)", uiPort), "OK", "em execução / ativa"})
	}
	if adminRunning {
		checks = append(checks, Check{"Caddy devlan.localhost", "OK", fmt.Sprintf("reverse proxy para 127.0.0.1:%d ativo", uiPort)})
	} else {
		checks = append(checks, Check{"Caddy devlan.localhost", "WARN", "Caddy WSL único parado; execute `devlan reload`"})
	}
	checks = append(checks, Check{"Compatibilidade de Versão", "OK", fmt.Sprintf("ProtocolVersion=%d", domain.ProtocolVersion)})

	effective, err := a.EffectiveConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	projects := effective.Projects
	if projectName != "" {
		project, found := effective.Project(projectName)
		if !found {
			return nil, fmt.Errorf("projeto não encontrado: %s", projectName)
		}
		projects = []domain.Project{project}
	}
	now := a.now()
	for _, project := range projects {
		resolved, err := effective.Resolve(project.Name)
		if err != nil {
			return nil, err
		}

		// Local name and origin validation
		if _, err := domain.NormalizeName(project.Name); err != nil {
			checks = append(checks, Check{"Projeto " + project.Name + " (Nome Local)", "FAIL", "nome inválido: " + err.Error()})
		} else {
			checks = append(checks, Check{"Projeto " + project.Name + " (Nome Local)", "OK", domain.LocalDevURL(project.Name)})
		}

		// LAN port validation
		overrideStr := "automática"
		if project.RoutePort != nil {
			overrideStr = "customizada"
		}
		if resolved.RoutePort < 1024 || resolved.RoutePort > 65535 {
			checks = append(checks, Check{"Projeto " + project.Name + " (Porta LAN)", "FAIL", fmt.Sprintf("porta %d inválida", resolved.RoutePort)})
		} else {
			checks = append(checks, Check{"Projeto " + project.Name + " (Porta LAN)", "OK", fmt.Sprintf(":%d (%s)", resolved.RoutePort, overrideStr)})
		}

		routeDetail := fmt.Sprintf("porta LAN :%d", resolved.RoutePort)

		if effective.IsExposureExpired(project, now) {
			checks = append(checks, Check{"Projeto " + project.Name + " (Exposição)", "WARN", "exposição temporária expirada"})
		}

		switch resolved.Mode {
		case domain.ModePHP:
			detected, detectErr := a.Detector.DetectPHP(ctx, project.Path)
			if detectErr != nil {
				checks = append(checks, Check{"Projeto " + project.Name, "FAIL", detectErr.Error()})
			} else {
				checks = append(checks, Check{"Projeto " + project.Name, "OK", fmt.Sprintf("%s, rota=%s, preset=%s, PHP=%s, pool=%s", detected.DocumentRoot, routeDetail, effective.PHPProjectPreset(project), effective.EffectivePHPVersion(project), phpconfig.PoolSummary(effective, project))})
			}
		case domain.ModeStatic:
			staticRoot := effective.StaticDocumentRoot(project)
			spa := effective.SPAFallback(project)
			checks = append(checks, Check{"Projeto " + project.Name, "OK", fmt.Sprintf("estático: %s (rota=%s, spa_fallback=%t)", staticRoot, routeDetail, spa)})
		case domain.ModeDev:
			devPort := effective.DevPort(project)
			devCmd := effective.DevCommand(project)
			pm := effective.PackageManager(project)
			statusStr := "parado"
			if a.Dev != nil {
				st, _ := a.Dev.Status(ctx, project, devPort)
				statusStr = string(st.State)
			}
			checks = append(checks, Check{"Projeto " + project.Name, "OK", fmt.Sprintf("dev server: %s (porta dev %d, rota=%s, pm=%s, status=%s)", devCmd, devPort, routeDetail, pm, statusStr)})
		}
	}
	return checks, nil
}
