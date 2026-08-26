package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	localapi "github.com/dougkusanagi/dev-lan/internal/api"
	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/metrics"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type ProjectView = localapi.ProjectView
type SystemStatusView = localapi.SystemStatusView
type OverviewView = localapi.OverviewView
type PHPVersionView = localapi.PHPVersionView
type DoctorCheckView = localapi.DoctorCheckView
type GlobalConfigView = localapi.GlobalConfigView
type ProjectConfigUpdate = localapi.ProjectConfigUpdate

type App struct {
	service *app.App
	ctx     context.Context
	api     *localapi.Server
	ownsAPI bool
}

func NewApp(service *app.App) *App {
	return &App{service: service, api: localapi.New(service)}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		return
	}
	if _, err := a.api.Start(); err == nil {
		a.ownsAPI = true
	} else if !errors.Is(err, localapi.ErrAlreadyRunning) {
		_ = a.service.Store.AppendSecurityAudit("API_GUI_WARN", "UI não iniciou a API local: "+err.Error())
	}
	go func() {
		startupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = a.service.Reload(startupCtx)
	}()
}

func (a *App) Shutdown(ctx context.Context) {
	_ = a.service.CloseDevProxies()
	if a.ownsAPI {
		_ = a.api.Close(ctx)
		a.ownsAPI = false
	}
}

func (a *App) GetProjects(filter string) ([]ProjectView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return localapi.BuildProjectViews(ctx, a.service, filter)
}

func (a *App) GetStatus() (SystemStatusView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return localapi.BuildStatusView(ctx, a.service)
}

func (a *App) GetOverview(filter string) (OverviewView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return localapi.BuildOverviewView(ctx, a.service, filter)
}

func (a *App) GetMetrics(project, rawRange string) (*metrics.Snapshot, error) {
	return localapi.BuildMetricsSnapshot(a.service, project, rawRange)
}

func (a *App) GetGlobalConfig() (GlobalConfigView, error) {
	return localapi.BuildGlobalConfigView(a.service)
}

func (a *App) GetPHPVersions() ([]PHPVersionView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return localapi.BuildPHPVersionsView(ctx, a.service)
}

func (a *App) InstallPHPVersion(version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, err := a.service.PHPInstall(ctx, version, nil)
	return err
}

func (a *App) RemovePHPVersion(version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := a.service.PHPRemove(ctx, version)
	return err
}

func (a *App) SetDefaultPHPVersion(version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := a.service.SetDefaultPHPVersion(ctx, version)
	return err
}

func (a *App) SaveGlobalConfig(view GlobalConfigView) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg, err := a.service.Store.Load()
	if err != nil {
		return err
	}

	if view.DefaultMode != "" {
		m, err := domain.ParseMode(view.DefaultMode)
		if err != nil {
			return err
		}
		cfg.DefaultMode = m
	}
	if view.WindowsPort > 0 {
		cfg.WindowsPort = view.WindowsPort
	}
	if view.HTTPSPort > 0 {
		cfg.HTTPSPort = view.HTTPSPort
	}
	cfg.TLSEnabled = view.TLSEnabled
	if view.PHPDefaultVersion != "" {
		cfg.PHPDefaultVersion = view.PHPDefaultVersion
	}
	cfg.Allowlist = view.Allowlist

	_, err = a.service.SaveConfigAndApply(ctx, cfg, true)
	return err
}

func (a *App) SaveProjectConfig(update ProjectConfigUpdate) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if update.Mode != "" {
		mode, err := domain.ParseMode(update.Mode)
		if err != nil {
			return err
		}
		if _, err := a.service.SetProjectMode(ctx, update.Name, &mode); err != nil {
			return err
		}
	}
	if update.TLSEnabled != nil {
		if _, _, err := a.service.SetProjectTLS(ctx, update.Name, *update.TLSEnabled); err != nil {
			return err
		}
	}
	if update.PHPVersion != "" {
		if _, err := a.service.SetProjectPHPVersion(ctx, update.Name, update.PHPVersion); err != nil {
			return err
		}
	}
	if update.PHPPreset != "" {
		if _, err := a.service.SetProjectPHPPreset(ctx, update.Name, update.PHPPreset); err != nil {
			return err
		}
	}
	if update.RoutePortAuto || update.RoutePort != nil {
		var port *int
		if !update.RoutePortAuto {
			port = update.RoutePort
		}
		if _, err := a.service.SetRoutePort(ctx, update.Name, port); err != nil {
			return err
		}
	}
	if update.StaticDir != "" {
		if _, err := a.service.SetProjectStaticDir(ctx, update.Name, update.StaticDir); err != nil {
			return err
		}
	}
	if update.DevCommand != "" {
		if _, err := a.service.SetProjectDevCommand(ctx, update.Name, update.DevCommand); err != nil {
			return err
		}
	}
	if update.DevPort > 0 {
		if _, err := a.service.SetProjectDevPort(ctx, update.Name, update.DevPort); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) LinkProject(name, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _, err := a.service.Link(ctx, name, path)
	return err
}

func (a *App) UnlinkProject(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _, err := a.service.Unlink(ctx, name)
	return err
}

func (a *App) HideProject(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := a.service.IgnoreProject(ctx, name)
	return err
}

func (a *App) ParkDir(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _, err := a.service.Park(ctx, path)
	return err
}

func (a *App) UnparkDir(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _, err := a.service.Unpark(ctx, path)
	return err
}

func (a *App) StartDev(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.service.StartDev(ctx, name)
}

func (a *App) StopDev(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return a.service.StopDev(ctx, name)
}

func (a *App) RestartDev(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.service.RestartDev(ctx, name)
}

func (a *App) BuildProject(name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return a.service.BuildProject(ctx, name)
}

func (a *App) InstallDeps(name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return a.service.InstallDeps(ctx, name)
}

func (a *App) GetProjectLogs(name string, lines int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if lines <= 0 {
		lines = 100
	}
	devLogs, err := a.service.ProjectDevLogs(ctx, name, lines)
	if err == nil && strings.TrimSpace(devLogs) != "" {
		return devLogs, nil
	}
	globalLogs, globalErr := a.service.Logs("devlan")
	if globalErr == nil && strings.TrimSpace(globalLogs) != "" {
		return fmt.Sprintf("Nenhum log de servidor dev disponível para %s.\n\nEventos do DevLAN:\n%s", name, globalLogs), nil
	}
	return fmt.Sprintf("Nenhum log de servidor dev disponível para %s.\nO servidor ainda não foi iniciado ou o projeto não usa o modo dev.", name), nil
}

func (a *App) RunDoctor(name string) ([]DoctorCheckView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return localapi.BuildDoctorChecksView(ctx, a.service, name)
}

func (a *App) ApplyDoctorFix(action, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch action {
	case "reload":
		_, err := a.service.Reload(ctx)
		return err
	case "firewall":
		return a.service.ReconcileFirewall(ctx)
	case "restart-dev":
		if target != "" {
			return a.service.RestartDev(ctx, target)
		}
		return nil
	default:
		_, err := a.service.Reload(ctx)
		return err
	}
}

func (a *App) OpenURL(rawURL string) error {
	if a.ctx != nil {
		wailsruntime.BrowserOpenURL(a.ctx, rawURL)
		return nil
	}
	return openBrowser(rawURL)
}

func (a *App) CopyURL(rawURL string) error {
	if a.ctx != nil {
		return wailsruntime.ClipboardSetText(a.ctx, rawURL)
	}
	return nil
}

func (a *App) Reload() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := a.service.Reload(ctx)
	return err
}

func (a *App) ExportConfigJSON() (string, error) {
	data, err := a.service.ExportConfig()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a *App) ExportDiagnostic() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.service.DiagnosticBundle(ctx, "")
}

func (a *App) TrustCA() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.service.Trust(ctx)
}

func (a *App) GetSecurityAudit(lines int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.service.SecurityAuditLogs(ctx, lines)
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}
