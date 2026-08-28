package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/metrics"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

// The methods in this file are the composition boundary consumed by HTTP,
// CLI and Wails. They adapt the existing App implementation to value-only
// application contracts; adapter fields remain private to this package.

func (a *App) EndpointFilesSnapshot() application.EndpointFiles {
	return a.APIEndpointFiles()
}

func (a *App) ManagedPathsSnapshot() application.ManagedPaths {
	paths := a.Store.Paths()
	return application.ManagedPaths{
		Dir: paths.Dir, LogsDir: paths.LogsDir, Caddy: paths.Caddy,
		WindowsCaddy: paths.WindowsCaddy, WSLCaddy: paths.WSLCaddy,
		SecurityLog: paths.SecurityLog, CARootExport: paths.CARootExport,
	}
}

func (a *App) LANAddressSnapshot() string {
	cfg, err := a.Config()
	if err != nil || cfg.LANAddress == "" {
		return "localhost"
	}
	if host, err := a.resourceUseCases().LANAddress(context.Background(), cfg.LANAddress); err == nil && host != "" {
		return host
	}
	return "localhost"
}

func (a *App) ClockNow() time.Time {
	return a.resourceUseCases().Now()
}

func (a *App) WSLStatsSnapshot() application.WSLStats {
	stats := a.WSL.StatsSnapshot()
	return application.WSLStats{
		TotalCalls: stats.TotalCalls, TotalFailures: stats.TotalFailures,
		TotalCanceled: stats.TotalCanceled, TotalDuration: stats.TotalDuration,
	}
}

func (a *App) CaddyStatusSnapshot(ctx context.Context) application.CaddyStatus {
	return applicationCaddyStatus(a.CaddyStatus(ctx))
}

func (a *App) TopologyStatusSnapshot(ctx context.Context) application.TopologyStatus {
	return applicationTopologyStatus(a.CaddyTopologyStatus(ctx))
}

func (a *App) CompatibilityReport(ctx context.Context) application.WSLCompatibilityReport {
	return applicationCompatibilityReport(a.WSLCompatibility(ctx))
}

func (a *App) TopologySnapshot(ctx context.Context) application.TopologySnapshot {
	return a.Topology(ctx)
}

func (a *App) NetworkProfileSnapshot(ctx context.Context) application.NetworkProfile {
	profile, _ := a.resourceUseCases().NetworkProfile(ctx)
	return application.NetworkProfile{Public: profile.Public, Detail: profile.Detail}
}

func (a *App) ProjectRuntime(ctx context.Context) (application.ProjectRuntimeSnapshot, error) {
	cfg, err := a.Config()
	if err != nil {
		return application.ProjectRuntimeSnapshot{}, err
	}
	effective, err := a.EffectiveConfig(ctx, cfg)
	if err != nil {
		return application.ProjectRuntimeSnapshot{}, err
	}
	caddy := a.CaddyStatusSnapshot(ctx)
	edgeReady := caddy.Running || caddy.Live
	wslReady := caddy.Available
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		edgeReady, wslReady = true, true
	}
	host := cfg.LANAddress
	if lanHost, lanErr := a.resourceUseCases().LANAddress(ctx, host); lanErr == nil {
		host = lanHost
	} else if host == "auto" || host == "" {
		host = "localhost"
	}

	snapshot := application.ProjectRuntimeSnapshot{
		Config: cfg, Effective: effective, EdgeReady: edgeReady, WSLReady: wslReady,
		Caddy: caddy, Host: host, Sockets: make(map[string]bool),
		DevStatuses: make(map[string]application.DevProcessStatus),
	}
	socketPaths := make([]string, 0, len(effective.Projects))
	for _, project := range effective.Projects {
		resolved, resolveErr := effective.Resolve(project.Name)
		if resolveErr == nil && resolved.Mode == domain.ModePHP && wslReady && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
			socketPaths = append(socketPaths, effective.PHPSocket(project))
		}
	}
	if len(socketPaths) > 0 {
		values, socketErr := a.WSL.IsSockets(
			platform.WithWSLOperation(ctx, platform.WSLOperationStatus), socketPaths...,
		)
		snapshot.Sockets = values
		if socketErr != nil {
			snapshot.SocketError = socketErr.Error()
		}
	}
	if statuses, statusErr := a.DevStatuses(
		platform.WithWSLOperation(ctx, platform.WSLOperationStatus), effective, effective.Projects,
	); statusErr == nil || len(statuses) > 0 {
		for name, status := range statuses {
			snapshot.DevStatuses[name] = applicationDevStatus(status)
		}
	}
	return snapshot, nil
}

func (a *App) DevStatusSnapshot(ctx context.Context, selector string) (application.DevProcessStatus, error) {
	status, err := a.DevStatus(ctx, selector)
	if err != nil {
		return application.DevProcessStatus{}, err
	}
	return applicationDevStatus(status), nil
}

func (a *App) DoctorSnapshot(ctx context.Context, projectName string) ([]application.DoctorCheck, error) {
	checks, err := a.Doctor(ctx, projectName)
	if err != nil {
		return nil, err
	}
	result := make([]application.DoctorCheck, 0, len(checks))
	for _, check := range checks {
		result = append(result, application.DoctorCheck{Name: check.Name, Status: check.Status, Detail: check.Detail})
	}
	return result, nil
}

func applicationDevStatus(status platform.DevProcessStatus) application.DevProcessStatus {
	return application.DevProcessStatus{
		ProjectName: status.ProjectName, Port: status.Port, State: string(status.State),
		PID: status.PID, Output: status.Output,
	}
}

func (a *App) SystemHealth(ctx context.Context, caddy application.CaddyStatus) application.SystemHealthSnapshot {
	cfg, err := a.Config()
	if err != nil {
		return application.SystemHealthSnapshot{}
	}
	phpVersions, _ := a.PHPVersions(platform.WithWSLOperation(ctx, platform.WSLOperationStatus))
	firewallOK, _ := a.FirewallHealthy(ctx, cfg)
	hyperVOK := false
	spec := platform.FirewallSpecForConfig(cfg)
	switch composite := a.Firewall.(type) {
	case platform.CompositeFirewall:
		status := composite.HyperVStatus(ctx, spec)
		hyperVOK = !status.Supported || status.Healthy
	case *platform.CompositeFirewall:
		if composite != nil {
			status := composite.HyperVStatus(ctx, spec)
			hyperVOK = !status.Supported || status.Healthy
		}
	}
	caValid, caTrusted := false, false
	if caInfo, caErr := a.CAInfo(ctx); caErr == nil {
		caValid = caInfo["valid"] == "true"
		caTrusted = caInfo["trusted"] == "true"
	}
	compatibility := application.WSLCompatibilityReport{}
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		configured := a.mirroredNetworkingConfigured()
		compatibility.MirroredConfigured = configured
		compatibility.MirroredNetworking = configured
	} else {
		compatibility = a.CompatibilityReport(ctx)
	}
	return application.SystemHealthSnapshot{
		Topology:     applicationTopologyStatus(a.CaddyTopologyStatus(ctx)),
		WSLAvailable: a.wslAvailable(ctx), FirewallOK: firewallOK, HyperVOK: hyperVOK,
		CAValid: caValid, CATrusted: caTrusted,
		MirroredConfigured: compatibility.MirroredConfigured,
		MirroredNetworking: compatibility.MirroredNetworking,
		PHPVersions:        append([]application.PHPVersionStatus(nil), phpVersions...),
	}
}

func (a *App) wslAvailable(ctx context.Context) bool {
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		return true
	}
	found, err := a.WSL.HasCommands(
		platform.WithWSLOperation(ctx, platform.WSLOperationStatus), "bash", "caddy",
	)
	return err == nil && (found["bash"] || found["caddy"])
}

func (a *App) mirroredNetworkingConfigured() bool {
	path := a.WSLConfigPath
	if path == "" {
		path = platform.UserWSLConfigPath()
	}
	data, err := os.ReadFile(path)
	return err == nil && platform.WSLConfigHasMirroredNetworking(string(data))
}

func (a *App) MetricsSnapshot(ctx context.Context, project, rawRange string) (*metrics.Snapshot, error) {
	rangeValue := metrics.Range(rawRange)
	if rangeValue != metrics.Range15m && rangeValue != metrics.Range1h && rangeValue != metrics.Range24h && rangeValue != metrics.Range7d {
		return nil, errors.New("intervalo de métricas inválido: " + rawRange)
	}
	accessLog := filepath.Join(a.Store.Paths().LogsDir, "access.jsonl")
	a.metricsMu.Lock()
	if a.metricsCollectors == nil {
		a.metricsCollectors = make(map[string]*metrics.Collector)
	}
	collector := a.metricsCollectors[accessLog]
	if collector == nil {
		collector = metrics.NewCollector()
		a.metricsCollectors[accessLog] = collector
	}
	a.metricsMu.Unlock()
	return collector.Snapshot(accessLog, project, rangeValue, a.ClockNow())
}

func (a *App) TelemetryStatus() (application.TelemetryStatus, error) {
	consent, err := a.Telemetry.Load()
	if err != nil {
		return application.TelemetryStatus{}, err
	}
	queued, err := a.Telemetry.QueueSize()
	if err != nil {
		return application.TelemetryStatus{}, err
	}
	return application.TelemetryStatus{Enabled: consent.Enabled, Endpoint: consent.Endpoint, Queued: queued}, nil
}

func (a *App) SetTelemetryConsent(enabled bool, endpoint string) error {
	return a.Telemetry.SetConsent(enabled, endpoint)
}

func (a *App) SendTelemetry(ctx context.Context) (int, error) {
	return a.Telemetry.Send(ctx)
}

func (a *App) MigrateTopology(ctx context.Context, confirmed bool) (application.MigrationResult, error) {
	result, err := a.MigrateToSingleCaddy(ctx, confirmed)
	if errors.Is(err, platform.ErrWSLShutdownConfirmation) {
		return application.MigrationResult{}, application.ErrWSLShutdownConfirmation
	}
	converted := application.MigrationResult{
		Topology: string(result.Topology), BackupDir: result.BackupDir,
		UnifiedBackupPath: result.UnifiedBackupPath, RolledBack: result.RolledBack,
		Steps: make([]application.MigrationStep, 0, len(result.Steps)),
	}
	for _, step := range result.Steps {
		converted.Steps = append(converted.Steps, application.MigrationStep(step))
	}
	return converted, err
}

var _ application.ProjectCommandPort = (*App)(nil)
var _ application.SettingsCommandPort = (*App)(nil)
var _ application.ExtendedCommandPort = (*App)(nil)
var _ application.QueryPort = (*App)(nil)
var _ application.ExtendedQueryPort = (*App)(nil)
