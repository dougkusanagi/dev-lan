package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/metrics"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

type projectViewRuntime struct {
	cfg         domain.Config
	effective   domain.Config
	edgeReady   bool
	wslReady    bool
	caddyStatus platform.CaddyServiceStatus
	host        string
	sockets     map[string]bool
	socketErr   error
	devStatuses map[string]platform.DevProcessStatus
}

type systemHealthSnapshot struct {
	topology           platform.TopologySnapshot
	wslAvailable       bool
	firewallOK         bool
	hyperVOK           bool
	caValid            bool
	caTrusted          bool
	mirroredConfigured bool
	mirroredNetworking bool
	phpVersions        []app.PHPVersionStatus
}

func loadProjectViewRuntimeUncached(ctx context.Context, service *app.App, queries *application.Queries) (*projectViewRuntime, error) {
	cfg, err := queries.Config(ctx)
	if err != nil {
		return nil, err
	}
	effective, err := queries.EffectiveConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	caddyStatus := serviceCaddyStatus(ctx, service)
	edgeReady := caddyStatus.Running || caddyStatus.Live
	wslReady := caddyStatus.Available
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
		caddyStatus: caddyStatus,
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

func loadProjectViewRuntime(ctx context.Context, service *app.App, queries *application.Queries, cache *ReadModelCache) (*projectViewRuntime, bool, error) {
	now := time.Now()
	if service.Now != nil {
		now = service.Now()
	}
	value, hit, err := cache.cachedHot(ctx, service, queries, now)
	return value, hit, err
}

func loadSystemHealthSnapshot(ctx context.Context, service *app.App, queries *application.Queries, caddyStatus platform.CaddyServiceStatus) systemHealthSnapshot {
	cfg, err := queries.Config(ctx)
	if err != nil {
		return systemHealthSnapshot{}
	}
	phpVersions, _ := service.PHPVersions(platform.WithWSLOperation(ctx, platform.WSLOperationStatus))
	firewallOK, _ := service.FirewallHealthy(ctx, cfg)
	hyperVOK := false
	if composite, ok := service.Firewall.(platform.CompositeFirewall); ok {
		status := composite.HyperVStatus(ctx, platform.FirewallSpecForConfig(cfg))
		hyperVOK = !status.Supported || status.Healthy
	} else if composite, ok := service.Firewall.(*platform.CompositeFirewall); ok && composite != nil {
		status := composite.HyperVStatus(ctx, platform.FirewallSpecForConfig(cfg))
		hyperVOK = !status.Supported || status.Healthy
	}
	caValid, caTrusted := false, false
	if caInfo, caErr := service.CAInfo(ctx); caErr == nil {
		caValid = caInfo["valid"] == "true"
		caTrusted = caInfo["trusted"] == "true"
	}
	compatibility := platform.WSLCompatibilityReport{}
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		configured := mirroredNetworkingConfigured(service)
		compatibility.MirroredConfigured = configured
		compatibility.MirroredNetworking = configured
	} else {
		compatibility = service.WSLCompatibility(ctx)
	}
	return systemHealthSnapshot{
		topology:           cachedCaddyTopology(service, caddyStatus),
		wslAvailable:       wslAvailability(ctx, service),
		firewallOK:         firewallOK,
		hyperVOK:           hyperVOK,
		caValid:            caValid,
		caTrusted:          caTrusted,
		mirroredConfigured: compatibility.MirroredConfigured,
		mirroredNetworking: compatibility.MirroredNetworking,
		phpVersions:        phpVersions,
	}
}

func cachedCaddyTopology(service *app.App, status platform.CaddyServiceStatus) platform.TopologySnapshot {
	paths := service.ManagedPaths()
	_, unifiedErr := os.Stat(paths.Caddy)
	_, windowsErr := os.Stat(paths.WindowsCaddy)
	_, wslErr := os.Stat(paths.WSLCaddy)
	return platform.DetectCaddyTopology(unifiedErr == nil, windowsErr == nil, wslErr == nil, false, status.Running)
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
			Revision:        cfg.Revision,
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
				missing = append(missing, "Caddy WSL único")
			}
			if !runtime.wslReady {
				missing = append(missing, "execution plane WSL")
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

func buildProjectViews(ctx context.Context, service *app.App, queries *application.Queries, cache *ReadModelCache, filter string) ([]ProjectView, error) {
	runtime, _, err := loadProjectViewRuntime(ctx, service, queries, cache)
	if err != nil {
		return nil, err
	}
	return renderProjectViews(runtime, filter), nil
}

// BuildProjectViews is a convenience for callers that do not own a server
// lifecycle. API and Wails paths use Server.BuildProjectViews so their cache
// remains attached to the owning Server instance.
func BuildProjectViews(ctx context.Context, service *app.App, filter string) ([]ProjectView, error) {
	return buildProjectViews(ctx, service, application.NewQueries(service), NewReadModelCache(), filter)
}

func (s *Server) BuildProjectViews(ctx context.Context, filter string) ([]ProjectView, error) {
	return buildProjectViews(ctx, s.service, s.queries, s.readModelCache, filter)
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

func buildSystemStatusView(cfg domain.Config, phpVersions []app.PHPVersionStatus, caddyStatus platform.CaddyServiceStatus, health systemHealthSnapshot, observedAt string) SystemStatusView {
	host := cfg.LANAddress
	if host == "auto" {
		var lanErr error
		host, lanErr = platform.LANAddress()
		if lanErr != nil {
			host = "localhost"
		}
	}

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
		WindowsCaddyRunning: false,
		WSLCaddyRunning:     caddyStatus.Running,
		CaddyRunning:        caddyStatus.Running,
		CaddyTopology:       string(health.topology.Topology),
		CaddySystemd:        caddyStatus.Systemd,
		CaddyLive:           caddyStatus.Live,
		MirroredConfigured:  health.mirroredConfigured,
		MirroredNetworking:  health.mirroredNetworking,
		HyperVFirewallOk:    health.hyperVOK,
		CARootValid:         health.caValid,
		CARootTrusted:       health.caTrusted,
		WSLAvailable:        health.wslAvailable,
		FirewallOk:          health.firewallOK,
		PHPVersions:         vers,
		TotalProjects:       len(cfg.Projects),
		ProtocolVersion:     ProtocolVersion,
		Revision:            cfg.Revision,
		ObservedAt:          observedAt,
	}
}

// BuildTopologyView is the explicit M8 diagnostic boundary. The aggregate
// status remains compact for polling; this endpoint carries the detailed,
// independently observable state needed by doctor/repair and support tools.
func BuildTopologyView(ctx context.Context, service *app.App) app.TopologySnapshot {
	return service.Topology(ctx)
}

func serviceCaddyStatus(ctx context.Context, service *app.App) platform.CaddyServiceStatus {
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		return platform.CaddyServiceStatus{Available: true, Running: true, Live: true, AdminAddress: platform.UnifiedCaddyAdminAddress}
	}
	// The App owns the Caddy adapter; the status method stays behind the
	// application boundary so HTTP/Wails/read models cannot accidentally probe
	// a second Windows edge.
	return service.CaddyStatus(ctx)
}

func mirroredNetworkingConfigured(service *app.App) bool {
	path := service.WSLConfigPath
	if path == "" {
		path = platform.UserWSLConfigPath()
	}
	data, err := os.ReadFile(path)
	return err == nil && platform.WSLConfigHasMirroredNetworking(string(data))
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
func buildOverviewView(ctx context.Context, service *app.App, queries *application.Queries, cache *ReadModelCache, filter string) (OverviewView, error) {
	ctx = platform.WithWSLOperation(ctx, platform.WSLOperationPolling)
	started := time.Now()
	beforeStats := service.WSL.StatsSnapshot()
	runtime, hotHit, err := loadProjectViewRuntime(ctx, service, queries, cache)
	if err != nil {
		return OverviewView{}, err
	}
	now := time.Now()
	if service.Now != nil {
		now = service.Now()
	}
	health, coldHit := cache.cachedCold(ctx, service, queries, now, runtime.caddyStatus)
	observedAt := now.UTC().Format(time.RFC3339Nano)
	hotAge, coldAge := cache.ages(now)
	afterStats := service.WSL.StatsSnapshot()
	cacheStatus := "miss"
	if hotHit && coldHit {
		cacheStatus = "hot+cold"
	} else if hotHit {
		cacheStatus = "hot"
	} else if coldHit {
		cacheStatus = "cold"
	}
	return OverviewView{
		Projects:    renderProjectViews(runtime, filter),
		Status:      buildSystemStatusView(runtime.cfg, health.phpVersions, runtime.caddyStatus, health, observedAt),
		PHPVersions: phpVersionViews(health.phpVersions),
		Revision:    runtime.cfg.Revision,
		ObservedAt:  observedAt,
		Meta: &OverviewMeta{
			Cache:              cacheStatus,
			HotAgeMs:           hotAge,
			ColdAgeMs:          coldAge,
			DurationMs:         time.Since(started).Milliseconds(),
			WSLCalls:           afterStats.TotalCalls,
			WSLCallsDelta:      afterStats.TotalCalls - beforeStats.TotalCalls,
			WSLDurationMs:      afterStats.TotalDuration.Milliseconds(),
			WSLDurationDeltaMs: (afterStats.TotalDuration - beforeStats.TotalDuration).Milliseconds(),
		},
	}, nil
}

func BuildOverviewView(ctx context.Context, service *app.App, filter string) (OverviewView, error) {
	return buildOverviewView(ctx, service, application.NewQueries(service), NewReadModelCache(), filter)
}

func (s *Server) BuildOverviewView(ctx context.Context, filter string) (OverviewView, error) {
	return buildOverviewView(ctx, s.service, s.queries, s.readModelCache, filter)
}

func buildStatusView(ctx context.Context, service *app.App, queries *application.Queries, cache *ReadModelCache) (SystemStatusView, error) {
	cfg, err := queries.Config(ctx)
	if err != nil {
		return SystemStatusView{}, err
	}
	now := time.Now()
	if service.Now != nil {
		now = service.Now()
	}
	caddyStatus := serviceCaddyStatus(ctx, service)
	health, _ := cache.cachedCold(ctx, service, queries, now, caddyStatus)
	return buildSystemStatusView(cfg, health.phpVersions, caddyStatus, health, now.UTC().Format(time.RFC3339Nano)), nil
}

func BuildStatusView(ctx context.Context, service *app.App) (SystemStatusView, error) {
	return buildStatusView(ctx, service, application.NewQueries(service), NewReadModelCache())
}

func (s *Server) BuildStatusView(ctx context.Context) (SystemStatusView, error) {
	return buildStatusView(ctx, s.service, s.queries, s.readModelCache)
}

func BuildGlobalConfigView(service *app.App) (GlobalConfigView, error) {
	return buildGlobalConfigView(application.NewQueries(service))
}

func buildGlobalConfigView(queries *application.Queries) (GlobalConfigView, error) {
	cfg, err := queries.Config(context.Background())
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

func (s *Server) BuildGlobalConfigView() (GlobalConfigView, error) {
	return buildGlobalConfigView(s.queries)
}

func buildPHPVersionsView(ctx context.Context, service *app.App, queries *application.Queries, cache *ReadModelCache) ([]PHPVersionView, error) {
	now := time.Now()
	if service.Now != nil {
		now = service.Now()
	}
	health, _ := cache.cachedCold(ctx, service, queries, now, serviceCaddyStatus(ctx, service))
	return phpVersionViews(health.phpVersions), nil
}

func BuildPHPVersionsView(ctx context.Context, service *app.App) ([]PHPVersionView, error) {
	return buildPHPVersionsView(ctx, service, application.NewQueries(service), NewReadModelCache())
}

func (s *Server) BuildPHPVersionsView(ctx context.Context) ([]PHPVersionView, error) {
	return buildPHPVersionsView(ctx, s.service, s.queries, s.readModelCache)
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
			} else if strings.Contains(c.Name, "mirrored") || strings.Contains(c.Name, "systemd") || strings.Contains(c.Name, "Hyper-V") {
				fixable = true
				fixAction = "topology-repair"
			} else if strings.Contains(c.Name, "CA Local") {
				fixable = true
				fixAction = "trust"
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
	now := time.Now()
	if service.Now != nil {
		now = service.Now()
	}
	accessLog := filepath.Join(service.ManagedPaths().LogsDir, "access.jsonl")
	collector, _ := metricsCollectors.LoadOrStore(accessLog, metrics.NewCollector())
	return collector.(*metrics.Collector).Snapshot(accessLog, project, rangeValue, now)
}

var metricsCollectors sync.Map
