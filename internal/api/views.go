package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/metrics"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

type projectViewRuntime struct {
	cfg         domain.Config
	effective   domain.Config
	edgeReady   bool
	wslReady    bool
	host        string
	sockets     map[string]bool
	socketErr   error
	devStatuses map[string]platform.DevProcessStatus
}

func loadProjectViewRuntime(ctx context.Context, service *app.App) (*projectViewRuntime, error) {
	cfg, err := service.Store.Load()
	if err != nil {
		return nil, err
	}
	effective, err := service.EffectiveConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	edgeReady := platform.IsAdminResponsive(platform.WindowsCaddyAdminAddress)
	wslReady := platform.IsAdminResponsive(platform.WSLCaddyAdminAddress)
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		edgeReady = true
		wslReady = true
	}

	host := cfg.LANAddress
	if host == "auto" {
		var lanErr error
		host, lanErr = platform.LANAddress()
		if lanErr != nil {
			host = "localhost"
		}
	}

	runtime := &projectViewRuntime{
		cfg:         cfg,
		effective:   effective,
		edgeReady:   edgeReady,
		wslReady:    wslReady,
		host:        host,
		sockets:     make(map[string]bool),
		devStatuses: make(map[string]platform.DevProcessStatus),
	}

	// Socket checks are grouped by request, so a poll has at most one WSL
	// execution for all PHP projects.
	socketPaths := make([]string, 0, len(effective.Projects))
	for _, project := range effective.Projects {
		resolved, resolveErr := effective.Resolve(project.Name)
		if resolveErr == nil && resolved.Mode == domain.ModePHP && wslReady && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
			socketPaths = append(socketPaths, effective.PHPSocket(project))
		}
	}
	if len(socketPaths) > 0 {
		runtime.sockets, runtime.socketErr = service.WSL.IsSockets(
			platform.WithWSLOperation(ctx, platform.WSLOperationStatus), socketPaths...,
		)
	}

	if statuses, statusErr := service.DevStatuses(
		platform.WithWSLOperation(ctx, platform.WSLOperationStatus), effective, effective.Projects,
	); statusErr == nil || len(statuses) > 0 {
		runtime.devStatuses = statuses
	}
	return runtime, nil
}

func renderProjectViews(runtime *projectViewRuntime, filter string) []ProjectView {
	effective := runtime.effective
	cfg := runtime.cfg
	filterLower := strings.ToLower(strings.TrimSpace(filter))
	result := make([]ProjectView, 0, len(effective.Projects))
	linkedProjects := make(map[string]struct{}, len(cfg.Projects))
	for _, linked := range cfg.Projects {
		linkedProjects[linked.Name] = struct{}{}
	}

	for _, project := range effective.Projects {
		if filterLower != "" &&
			!strings.Contains(strings.ToLower(project.Name), filterLower) &&
			!strings.Contains(strings.ToLower(project.Path), filterLower) {
			continue
		}

		resolved, err := effective.Resolve(project.Name)
		if err != nil {
			continue
		}

		tlsActive := effective.SecureProject(project)
		url := resolved.URL(runtime.host, cfg.WindowsPort, cfg.HTTPSPort, tlsActive)

		kind := "parked"
		if _, linked := linkedProjects[project.Name]; linked {
			kind = "linked"
		}

		framework := "generic"
		if resolved.Mode == domain.ModePHP {
			framework = string(effective.PHPProjectPreset(project))
		} else if project.DevFramework != nil && *project.DevFramework != "" {
			framework = *project.DevFramework
		}

		modeVal := ""
		if project.Mode != nil {
			modeVal = string(*project.Mode)
		}

		phpVer := ""
		if resolved.Mode == domain.ModePHP {
			phpVer = effective.EffectivePHPVersion(project)
		}

		staticDir := ""
		if project.StaticDir != nil {
			staticDir = *project.StaticDir
		}

		pm := ""
		if resolved.Mode == domain.ModeDev {
			pm = effective.PackageManager(project)
		}

		view := ProjectView{
			Name:            project.Name,
			Path:            project.Path,
			Kind:            kind,
			Mode:            modeVal,
			EffectiveMode:   string(resolved.Mode),
			Framework:       framework,
			URL:             url,
			LANURL:          url,
			LocalDevURL:     domain.LocalDevURL(project.Name),
			LocalDevState:   "available",
			LANPreviewState: "ready",
			TLSEnabled:      tlsActive,
			Port:            resolved.RoutePort,
			Status:          "ready",
			PHPVersion:      phpVer,
			PackageManager:  pm,
			StaticDir:       staticDir,
			DevRunning:      false,
		}
		if project.RoutePort != nil {
			view.RoutePortOverride = *project.RoutePort
		}
		devCapable := resolved.Mode == domain.ModeDev || resolved.Mode == domain.ModeAuto || (resolved.Mode == domain.ModePHP && effective.PHPProjectPreset(project) == domain.PHPPresetLaravel)
		if devCapable {
			view.LocalDevState = "stopped"
		}
		if !runtime.edgeReady || !runtime.wslReady {
			view.Status = "degraded"
			missing := make([]string, 0, 2)
			if !runtime.edgeReady {
				missing = append(missing, "Caddy Windows")
			}
			if !runtime.wslReady {
				missing = append(missing, "Caddy WSL")
			}
			view.StatusDetail = "infraestrutura indisponível: " + strings.Join(missing, ", ")
		}
		if resolved.Mode == domain.ModePHP && runtime.wslReady && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
			socket := effective.PHPSocket(project)
			if runtime.socketErr != nil || !runtime.sockets[socket] {
				view.Status = "degraded"
				view.StatusDetail = "socket PHP-FPM indisponível: " + socket
			}
		}

		// Use the grouped status snapshot. The previous per-row resolve path
		// caused park discovery to execute once per project.
		if devCapable {
			if devStatus, ok := runtime.devStatuses[project.Name]; ok {
				view.DevPort = devStatus.Port
				view.DevPid = devStatus.PID
				switch devStatus.State {
				case platform.StateRunning:
					view.DevRunning = true
					view.LocalDevState = "active"
					view.LANPreviewState = "paused"
					if view.Status != "degraded" {
						view.Status = "ready"
					}
				case platform.StateStarting:
					view.DevRunning = true
					view.LocalDevState = "starting"
					view.LANPreviewState = "paused"
					if view.Status != "degraded" {
						view.Status = "starting"
					}
				case platform.StateError:
					view.DevRunning = false
					view.Status = "error"
					view.StatusDetail = "servidor dev falhou; abra os logs para ver a saída"
				case platform.StateStopped:
					view.DevRunning = false
					if resolved.Mode == domain.ModeDev {
						view.Status = "stopped"
						view.StatusDetail = "servidor dev parado"
					}
				}
			}
		}

		result = append(result, view)
	}

	return result
}

func BuildProjectViews(ctx context.Context, service *app.App, filter string) ([]ProjectView, error) {
	runtime, err := loadProjectViewRuntime(ctx, service)
	if err != nil {
		return nil, err
	}
	return renderProjectViews(runtime, filter), nil
}

func wslAvailability(ctx context.Context, service *app.App) bool {
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		return true
	}
	found, err := service.WSL.HasCommands(
		platform.WithWSLOperation(ctx, platform.WSLOperationStatus), "bash", "caddy",
	)
	if err != nil {
		return false
	}
	return found["bash"] || found["caddy"]
}

func buildSystemStatusView(ctx context.Context, service *app.App, cfg domain.Config, phpVersions []app.PHPVersionStatus, wslAvailable bool) SystemStatusView {
	host := cfg.LANAddress
	if host == "auto" {
		var lanErr error
		host, lanErr = platform.LANAddress()
		if lanErr != nil {
			host = "localhost"
		}
	}

	winCaddyRunning := platform.IsAdminResponsive(platform.WindowsCaddyAdminAddress)
	wslCaddyRunning := platform.IsAdminResponsive(platform.WSLCaddyAdminAddress)
	firewallOk, _ := service.FirewallHealthy(ctx, cfg)
	vers := make([]string, 0, len(phpVersions))
	for _, version := range phpVersions {
		vers = append(vers, version.Version)
	}

	return SystemStatusView{
		LANIP:               host,
		WindowsPort:         cfg.WindowsPort,
		HTTPSPort:           cfg.HTTPSPort,
		RouteBasePort:       cfg.RouteBasePort,
		RoutePortCount:      cfg.RoutePortCount,
		UIPort:              cfg.UIPort,
		TLSEnabled:          cfg.TLSEnabled,
		DefaultMode:         string(cfg.DefaultMode),
		PHPDefaultVersion:   cfg.PHPDefaultVersion,
		WindowsCaddyRunning: winCaddyRunning,
		WSLCaddyRunning:     wslCaddyRunning,
		WSLAvailable:        wslAvailable,
		FirewallOk:          firewallOk,
		PHPVersions:         vers,
		TotalProjects:       len(cfg.Projects),
		ProtocolVersion:     ProtocolVersion,
	}
}

func phpVersionViews(items []app.PHPVersionStatus) []PHPVersionView {
	result := make([]PHPVersionView, 0, len(items))
	for _, item := range items {
		result = append(result, PHPVersionView{
			Version: item.Version, Installed: item.Installed, Configured: item.Configured, Extensions: item.Extensions,
		})
	}
	return result
}

// BuildOverviewView is the browser polling boundary. It materializes parks
// once and shares the resulting runtime/status/PHP snapshot across the three
// panels that used to issue independent requests.
func BuildOverviewView(ctx context.Context, service *app.App, filter string) (OverviewView, error) {
	ctx = platform.WithWSLOperation(ctx, platform.WSLOperationPolling)
	runtime, err := loadProjectViewRuntime(ctx, service)
	if err != nil {
		return OverviewView{}, err
	}
	phpItems, _ := service.PHPVersions(platform.WithWSLOperation(ctx, platform.WSLOperationStatus))
	return OverviewView{
		Projects:    renderProjectViews(runtime, filter),
		Status:      buildSystemStatusView(ctx, service, runtime.cfg, phpItems, wslAvailability(ctx, service)),
		PHPVersions: phpVersionViews(phpItems),
	}, nil
}

func BuildStatusView(ctx context.Context, service *app.App) (SystemStatusView, error) {
	cfg, err := service.Store.Load()
	if err != nil {
		return SystemStatusView{}, err
	}
	phpVers, _ := service.PHPVersions(platform.WithWSLOperation(ctx, platform.WSLOperationStatus))
	return buildSystemStatusView(ctx, service, cfg, phpVers, wslAvailability(ctx, service)), nil
}

func BuildGlobalConfigView(service *app.App) (GlobalConfigView, error) {
	cfg, err := service.Store.Load()
	if err != nil {
		return GlobalConfigView{}, err
	}
	return GlobalConfigView{
		DefaultMode:       string(cfg.DefaultMode),
		WindowsPort:       cfg.WindowsPort,
		HTTPSPort:         cfg.HTTPSPort,
		TLSEnabled:        cfg.TLSEnabled,
		PHPDefaultVersion: cfg.PHPDefaultVersion,
		Allowlist:         cfg.Allowlist,
	}, nil
}

func BuildPHPVersionsView(ctx context.Context, service *app.App) ([]PHPVersionView, error) {
	items, err := service.PHPVersions(platform.WithWSLOperation(ctx, platform.WSLOperationStatus))
	if err != nil {
		return nil, err
	}
	return phpVersionViews(items), nil
}

func BuildDoctorChecksView(ctx context.Context, service *app.App, name string) ([]DoctorCheckView, error) {
	checks, err := service.Doctor(ctx, name)
	if err != nil {
		return nil, err
	}

	result := make([]DoctorCheckView, 0, len(checks))
	for _, c := range checks {
		fixable := false
		fixAction := ""
		if c.Status != "OK" {
			if strings.Contains(c.Name, "Caddy") || strings.Contains(c.Name, "Config") {
				fixable = true
				fixAction = "reload"
			} else if strings.Contains(c.Name, "Firewall") {
				fixable = true
				fixAction = "firewall"
			} else if strings.Contains(c.Name, "Dev") {
				fixable = true
				fixAction = "restart-dev"
			}
		}

		result = append(result, DoctorCheckView{
			Name:      c.Name,
			Status:    c.Status,
			Detail:    c.Detail,
			Fixable:   fixable,
			FixAction: fixAction,
		})
	}
	return result, nil
}

func BuildMetricsSnapshot(service *app.App, project, rawRange string) (*metrics.Snapshot, error) {
	rangeValue := metrics.Range(rawRange)
	if rangeValue != metrics.Range15m && rangeValue != metrics.Range1h && rangeValue != metrics.Range24h && rangeValue != metrics.Range7d {
		return nil, fmt.Errorf("intervalo de métricas inválido: %s", rawRange)
	}
	data, err := os.ReadFile(filepath.Join(service.Store.Paths().LogsDir, "access.jsonl"))
	if err != nil {
		return nil, nil
	}
	now := time.Now()
	if service.Now != nil {
		now = service.Now()
	}
	return metrics.Aggregate(data, project, rangeValue, now), nil
}
