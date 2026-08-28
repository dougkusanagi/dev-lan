package api

import (
	"context"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/metrics"
)

type projectViewRuntime struct {
	cfg         domain.Config
	effective   domain.Config
	edgeReady   bool
	wslReady    bool
	caddyStatus application.CaddyStatus
	host        string
	sockets     map[string]bool
	socketErr   string
	devStatuses map[string]application.DevProcessStatus
}

func loadProjectViewRuntimeUncached(ctx context.Context, queries *application.Queries) (*projectViewRuntime, error) {
	snapshot, err := queries.ProjectRuntime(ctx)
	if err != nil {
		return nil, err
	}
	return &projectViewRuntime{
		cfg:         snapshot.Config,
		effective:   snapshot.Effective,
		edgeReady:   snapshot.EdgeReady,
		wslReady:    snapshot.WSLReady,
		caddyStatus: snapshot.Caddy,
		host:        snapshot.Host,
		sockets:     snapshot.Sockets,
		socketErr:   snapshot.SocketError,
		devStatuses: snapshot.DevStatuses,
	}, nil
}

func loadProjectViewRuntime(ctx context.Context, queries *application.Queries, cache *ReadModelCache) (*projectViewRuntime, bool, error) {
	now := queries.Now()
	value, hit, err := cache.cachedHot(ctx, queries, now)
	return value, hit, err
}

func loadSystemHealthSnapshot(ctx context.Context, queries *application.Queries, caddyStatus application.CaddyStatus) application.SystemHealthSnapshot {
	return queries.SystemHealth(ctx, caddyStatus)
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
		if resolved.Mode == domain.ModePHP && runtime.wslReady && (len(runtime.sockets) > 0 || runtime.socketErr != "") {
			socket := effective.PHPSocket(project)
			if !runtime.sockets[socket] {
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
				case application.DevStateRunning:
					view.DevRunning = true
					view.LocalDevState = "active"
					view.LANPreviewState = "paused"
					if view.Status != "degraded" {
						view.Status = "ready"
					}
				case application.DevStateStarting:
					view.DevRunning = true
					view.LocalDevState = "starting"
					view.LANPreviewState = "paused"
					if view.Status != "degraded" {
						view.Status = "starting"
					}
				case application.DevStateError:
					view.DevRunning = false
					view.Status = "error"
					view.StatusDetail = "servidor dev falhou; abra os logs para ver a saída"
				case application.DevStateStopped:
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

func buildProjectViews(ctx context.Context, queries *application.Queries, cache *ReadModelCache, filter string) ([]ProjectView, error) {
	runtime, _, err := loadProjectViewRuntime(ctx, queries, cache)
	if err != nil {
		return nil, err
	}
	return renderProjectViews(runtime, filter), nil
}

// BuildProjectViews is a convenience for callers that do not own a server
// lifecycle. API and Wails paths use Server.BuildProjectViews so their cache
// remains attached to the owning Server instance.
func BuildProjectViews(ctx context.Context, service *app.App, filter string) ([]ProjectView, error) {
	return buildProjectViews(ctx, application.NewQueries(service), NewReadModelCache(), filter)
}

func (s *Server) BuildProjectViews(ctx context.Context, filter string) ([]ProjectView, error) {
	return buildProjectViews(ctx, s.queries, s.readModelCache, filter)
}

func buildSystemStatusView(cfg domain.Config, phpVersions []application.PHPVersionStatus, caddyStatus application.CaddyStatus, health application.SystemHealthSnapshot, host, observedAt string) SystemStatusView {
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
		CaddyTopology:       health.Topology.Topology,
		CaddySystemd:        caddyStatus.Systemd,
		CaddyLive:           caddyStatus.Live,
		MirroredConfigured:  health.MirroredConfigured,
		MirroredNetworking:  health.MirroredNetworking,
		HyperVFirewallOk:    health.HyperVOK,
		CARootValid:         health.CAValid,
		CARootTrusted:       health.CATrusted,
		WSLAvailable:        health.WSLAvailable,
		FirewallOk:          health.FirewallOK,
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
	return application.NewQueries(service).Topology(ctx)
}

func phpVersionViews(items []application.PHPVersionStatus) []PHPVersionView {
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
func buildOverviewView(ctx context.Context, queries *application.Queries, cache *ReadModelCache, filter string) (OverviewView, error) {
	started := time.Now()
	beforeStats := queries.WSLStats()
	runtime, hotHit, err := loadProjectViewRuntime(ctx, queries, cache)
	if err != nil {
		return OverviewView{}, err
	}
	now := queries.Now()
	health, coldHit := cache.cachedCold(ctx, queries, now, runtime.caddyStatus)
	observedAt := now.UTC().Format(time.RFC3339Nano)
	hotAge, coldAge := cache.ages(now)
	afterStats := queries.WSLStats()
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
		Status:      buildSystemStatusView(runtime.cfg, health.PHPVersions, runtime.caddyStatus, health, runtime.host, observedAt),
		PHPVersions: phpVersionViews(health.PHPVersions),
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
	return buildOverviewView(ctx, application.NewQueries(service), NewReadModelCache(), filter)
}

func (s *Server) BuildOverviewView(ctx context.Context, filter string) (OverviewView, error) {
	return buildOverviewView(ctx, s.queries, s.readModelCache, filter)
}

func buildStatusView(ctx context.Context, queries *application.Queries, cache *ReadModelCache) (SystemStatusView, error) {
	cfg, err := queries.Config(ctx)
	if err != nil {
		return SystemStatusView{}, err
	}
	now := queries.Now()
	caddyStatus := queries.CaddyStatus(ctx)
	health, _ := cache.cachedCold(ctx, queries, now, caddyStatus)
	return buildSystemStatusView(cfg, health.PHPVersions, caddyStatus, health, queries.LANAddress(), now.UTC().Format(time.RFC3339Nano)), nil
}

func BuildStatusView(ctx context.Context, service *app.App) (SystemStatusView, error) {
	return buildStatusView(ctx, application.NewQueries(service), NewReadModelCache())
}

func (s *Server) BuildStatusView(ctx context.Context) (SystemStatusView, error) {
	return buildStatusView(ctx, s.queries, s.readModelCache)
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

func buildPHPVersionsView(ctx context.Context, queries *application.Queries, cache *ReadModelCache) ([]PHPVersionView, error) {
	now := queries.Now()
	health, _ := cache.cachedCold(ctx, queries, now, queries.CaddyStatus(ctx))
	return phpVersionViews(health.PHPVersions), nil
}

func BuildPHPVersionsView(ctx context.Context, service *app.App) ([]PHPVersionView, error) {
	return buildPHPVersionsView(ctx, application.NewQueries(service), NewReadModelCache())
}

func (s *Server) BuildPHPVersionsView(ctx context.Context) ([]PHPVersionView, error) {
	return buildPHPVersionsView(ctx, s.queries, s.readModelCache)
}

func buildDoctorChecksView(ctx context.Context, queries *application.Queries, name string) ([]DoctorCheckView, error) {
	checks, err := queries.Doctor(ctx, name)
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

func BuildDoctorChecksView(ctx context.Context, service *app.App, name string) ([]DoctorCheckView, error) {
	return buildDoctorChecksView(ctx, application.NewQueries(service), name)
}

func (s *Server) BuildDoctorChecksView(ctx context.Context, name string) ([]DoctorCheckView, error) {
	return buildDoctorChecksView(ctx, s.queries, name)
}

func BuildMetricsSnapshot(service *app.App, project, rawRange string) (*metrics.Snapshot, error) {
	return application.NewQueries(service).MetricsSnapshot(context.Background(), project, rawRange)
}

func (s *Server) BuildMetricsSnapshot(ctx context.Context, project, rawRange string) (*metrics.Snapshot, error) {
	return s.queries.MetricsSnapshot(ctx, project, rawRange)
}
