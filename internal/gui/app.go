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
	"github.com/dougkusanagi/dev-lan/internal/application"
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
type MutationResult = localapi.MutationResult

type App struct {
	commands              *application.Commands
	queries               *application.Queries
	ctx                   context.Context
	api                   *localapi.Server
	ownsAPI               bool
	operationEventsCancel context.CancelFunc
}

func NewApp(service *app.App) *App {
	commands := application.NewCommands(service, service)
	queries := application.NewQueries(service)
	return &App{commands: commands, queries: queries, api: localapi.NewWithApplication(service, commands, queries)}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		return
	}
	a.startOperationEvents()
	if _, err := a.api.Start(); err == nil {
		a.ownsAPI = true
	} else if !errors.Is(err, localapi.ErrAlreadyRunning) {
		a.commands.Audit("API_GUI_WARN", "UI não iniciou a API local: "+err.Error())
	}
	go func() {
		startupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = a.commands.Reload(startupCtx)
	}()
}

func (a *App) Shutdown(ctx context.Context) {
	if a.operationEventsCancel != nil {
		a.operationEventsCancel()
		a.operationEventsCancel = nil
	}
	_ = a.commands.CloseDevProxies()
	if a.ownsAPI {
		_ = a.api.Close(ctx)
		a.ownsAPI = false
	}
}

func (a *App) startOperationEvents() {
	if a.ctx == nil || a.operationEventsCancel != nil {
		return
	}
	eventCtx, cancel := context.WithCancel(context.Background())
	a.operationEventsCancel = cancel
	updates, stop := a.queries.SubscribeOperations(eventCtx)
	go func() {
		defer stop()
		for {
			select {
			case <-eventCtx.Done():
				return
			case state, open := <-updates:
				if !open {
					return
				}
				result := a.api.BuildOperationResult(context.Background(), state)
				eventName := "devlan:operation-progress"
				if guiTerminalPhase(state.Phase) && state.ProjectName != "" {
					eventName = "devlan:project-state-changed"
				}
				wailsruntime.EventsEmit(a.ctx, eventName, result)
			}
		}
	}()
}

func guiTerminalPhase(phase string) bool {
	switch phase {
	case "ready", "stopped", "failed", "rolled_back":
		return true
	default:
		return false
	}
}

func (a *App) GetProjects(filter string) ([]ProjectView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.api.BuildProjectViews(ctx, filter)
}

func (a *App) GetStatus() (SystemStatusView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.api.BuildStatusView(ctx)
}

// GetTopology exposes the detailed single-edge diagnostic model to the Wails
// shell. It delegates to the same read boundary as the HTTP API.
func (a *App) GetTopology() (application.TopologySnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.queries.Topology(ctx), nil
}

func (a *App) GetOverview(filter string) (OverviewView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.api.BuildOverviewView(ctx, filter)
}

func (a *App) GetMetrics(project, rawRange string) (*metrics.Snapshot, error) {
	return a.api.BuildMetricsSnapshot(context.Background(), project, rawRange)
}

func (a *App) GetGlobalConfig() (GlobalConfigView, error) {
	return a.api.BuildGlobalConfigView()
}

func (a *App) GetPHPVersions() ([]PHPVersionView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.api.BuildPHPVersionsView(ctx)
}

func (a *App) InstallPHPVersion(version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, err := a.commands.PHPInstall(ctx, version, nil)
	if err == nil {
		a.api.InvalidateColdReadModelCache()
	}
	return err
}

func (a *App) RemovePHPVersion(version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := a.commands.PHPRemove(ctx, version)
	if err == nil {
		a.api.InvalidateColdReadModelCache()
	}
	return err
}

func (a *App) SetDefaultPHPVersion(version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := a.commands.SetDefaultPHPVersion(ctx, version)
	if err == nil {
		a.api.InvalidateColdReadModelCache()
	}
	return err
}

func (a *App) SaveGlobalConfig(view GlobalConfigView) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := a.api.SaveGlobalSettings(ctx, application.GlobalSettings{
		DefaultMode:       view.DefaultMode,
		WindowsPort:       view.WindowsPort,
		HTTPSPort:         view.HTTPSPort,
		TLSEnabled:        view.TLSEnabled,
		PHPDefaultVersion: view.PHPDefaultVersion,
		Allowlist:         view.Allowlist,
	})
	if err == nil {
		a.api.InvalidateReadModelCache()
	}
	return err
}

func (a *App) SaveProjectConfig(update ProjectConfigUpdate) error {
	// A project TLS change can validate/reload Caddy and reconcile host
	// integration. Keep the UI request bounded, but leave enough headroom for a
	// cold WSL start instead of reporting an ambiguous timeout after commit.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if update.Mode != "" {
		mode, err := domain.ParseMode(update.Mode)
		if err != nil {
			return err
		}
		if _, err := a.api.SetProjectMode(ctx, update.Name, &mode); err != nil {
			return err
		}
	}
	if update.TLSEnabled != nil {
		if _, _, err := a.commands.SetProjectTLS(ctx, update.Name, *update.TLSEnabled); err != nil {
			return err
		}
	}
	if update.PHPVersion != "" {
		if _, err := a.commands.SetProjectPHPVersion(ctx, update.Name, update.PHPVersion); err != nil {
			return err
		}
	}
	if update.PHPPreset != "" {
		if _, err := a.commands.SetProjectPHPPreset(ctx, update.Name, update.PHPPreset); err != nil {
			return err
		}
	}
	if update.RoutePortAuto || update.RoutePort != nil {
		var port *int
		if !update.RoutePortAuto {
			port = update.RoutePort
		}
		if _, err := a.commands.SetRoutePort(ctx, update.Name, port); err != nil {
			return err
		}
	}
	if update.StaticDir != "" {
		if _, err := a.commands.SetProjectStaticDir(ctx, update.Name, update.StaticDir); err != nil {
			return err
		}
	}
	if update.DevCommand != "" {
		if _, err := a.commands.SetProjectDevCommand(ctx, update.Name, update.DevCommand); err != nil {
			return err
		}
	}
	if update.DevPort > 0 {
		if _, err := a.commands.SetProjectDevPort(ctx, update.Name, update.DevPort); err != nil {
			return err
		}
	}
	a.api.InvalidateReadModelCache()
	return nil
}

// SaveProjectConfigResult is the Wails mutation contract used by the modern
// shell. The legacy SaveProjectConfig method remains for CLI-compatible
// callers and older generated bindings.
func (a *App) SaveProjectConfigResult(update ProjectConfigUpdate, operationID string) (MutationResult, error) {
	if update.TLSEnabled != nil && update.Mode == "" && update.PHPVersion == "" && update.PHPPreset == "" &&
		update.RoutePort == nil && !update.RoutePortAuto && update.StaticDir == "" && update.DevCommand == "" && update.DevPort == 0 {
		return a.acceptProjectOperation("tls", update.Name, operationID, 90*time.Second,
			func(ctx context.Context) (uint64, []string, error) {
				result, _, err := a.commands.SetProjectTLS(ctx, update.Name, *update.TLSEnabled)
				return result.Revision, result.Warnings, err
			})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := a.SaveProjectConfig(update); err != nil {
		return MutationResult{}, err
	}
	return a.resultForCompleted("config", update.Name, operationID, ctx, nil), nil
}

func (a *App) GetOperation(operationID string) (MutationResult, error) {
	state, ok := a.queries.Operation(operationID)
	if !ok {
		return MutationResult{}, fmt.Errorf("operação não encontrada: %s", operationID)
	}
	return a.api.BuildOperationResult(context.Background(), state), nil
}

func (a *App) StartDevOperation(name, operationID string) (MutationResult, error) {
	return a.acceptProjectOperation("start", name, operationID, 90*time.Second,
		func(ctx context.Context) (uint64, []string, error) {
			err := a.commands.StartDev(ctx, name)
			return a.api.QueryRevision(context.Background()), nil, err
		})
}

func (a *App) StopDevOperation(name, operationID string) (MutationResult, error) {
	return a.acceptProjectOperation("stop", name, operationID, 45*time.Second,
		func(ctx context.Context) (uint64, []string, error) {
			err := a.commands.StopDev(ctx, name)
			return a.api.QueryRevision(context.Background()), nil, err
		})
}

func (a *App) RestartDevOperation(name, operationID string) (MutationResult, error) {
	return a.acceptProjectOperation("restart", name, operationID, 90*time.Second,
		func(ctx context.Context) (uint64, []string, error) {
			err := a.commands.RestartDev(ctx, name)
			return a.api.QueryRevision(context.Background()), nil, err
		})
}

type guiOperationWork func(context.Context) (uint64, []string, error)

func (a *App) acceptProjectOperation(operation, project, operationID string, timeout time.Duration, work guiOperationWork) (MutationResult, error) {
	if strings.TrimSpace(operationID) == "" {
		operationID = application.NewOperationID()
	} else if len(operationID) > 96 {
		operationID = operationID[:96]
	}
	state, existed, err := a.commands.BeginOperation(operationID, operation, project)
	if err != nil {
		return MutationResult{}, err
	}
	if !existed {
		a.commands.SetOperationTransport(operationID, "wails")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			phase := "applying"
			if operation == "start" || operation == "restart" {
				phase = "starting"
			} else if operation == "stop" {
				phase = "stopping"
			}
			a.commands.UpdateOperation(operationID, phase, phase, 0, nil, nil, nil)
			revision, warnings, workErr := work(ctx)
			terminal := "ready"
			if operation == "stop" {
				terminal = "stopped"
			}
			if workErr != nil {
				terminal = "failed"
				if errors.Is(workErr, context.DeadlineExceeded) || strings.Contains(strings.ToLower(workErr.Error()), "rolled_back") {
					terminal = "rolled_back"
				}
			}
			if operation == "start" || operation == "stop" || operation == "restart" {
				a.api.InvalidateHotReadModelCache()
			} else {
				a.api.InvalidateReadModelCache()
			}
			a.commands.UpdateOperation(operationID, terminal, terminal, revision, nil, warnings, workErr)
		}()
	}
	return a.api.BuildOperationResult(context.Background(), state), nil
}

func (a *App) resultForCompleted(operation, project, operationID string, ctx context.Context, warnings []string) MutationResult {
	if strings.TrimSpace(operationID) == "" {
		operationID = application.NewOperationID()
	}
	state, _, _ := a.commands.BeginOperation(operationID, operation, project)
	a.commands.SetOperationTransport(operationID, "wails")
	state = a.commands.UpdateOperation(operationID, "ready", "ready", a.api.QueryRevision(context.Background()), nil, warnings, nil)
	a.api.InvalidateReadModelCache()
	return a.api.BuildOperationResult(ctx, state)
}

func (a *App) LinkProject(name, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _, err := a.commands.LinkProject(ctx, application.LinkProjectCommand{Name: name, Path: path})
	if err == nil {
		a.api.InvalidateReadModelCache()
	}
	return err
}

func (a *App) UnlinkProject(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _, err := a.commands.UnlinkProject(ctx, application.UnlinkProjectCommand{Name: name})
	if err == nil {
		a.api.InvalidateReadModelCache()
	}
	return err
}

func (a *App) HideProject(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := a.commands.IgnoreProject(ctx, application.IgnoreProjectCommand{Selector: name})
	if err == nil {
		a.api.InvalidateReadModelCache()
	}
	return err
}

func (a *App) ParkDir(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _, err := a.commands.ParkDirectory(ctx, application.ParkDirectoryCommand{Path: path})
	if err == nil {
		a.api.InvalidateReadModelCache()
	}
	return err
}

func (a *App) UnparkDir(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _, err := a.commands.UnparkDirectory(ctx, application.UnparkDirectoryCommand{Path: path})
	if err == nil {
		a.api.InvalidateReadModelCache()
	}
	return err
}

func (a *App) StartDev(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := a.commands.StartDev(ctx, name)
	if err == nil {
		a.api.InvalidateHotReadModelCache()
	}
	return err
}

func (a *App) StopDev(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := a.commands.StopDev(ctx, name)
	if err == nil {
		a.api.InvalidateHotReadModelCache()
	}
	return err
}

func (a *App) RestartDev(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := a.commands.RestartDev(ctx, name)
	if err == nil {
		a.api.InvalidateHotReadModelCache()
	}
	return err
}

func (a *App) BuildProject(name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return a.commands.BuildProject(ctx, name)
}

func (a *App) InstallDeps(name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	return a.commands.InstallDeps(ctx, name)
}

func (a *App) GetProjectLogs(name string, lines int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if lines <= 0 {
		lines = 100
	}
	devLogs, err := a.queries.ProjectDevLogs(ctx, name, lines)
	if err == nil && strings.TrimSpace(devLogs) != "" {
		return devLogs, nil
	}
	globalLogs, globalErr := a.queries.Logs("devlan")
	if globalErr == nil && strings.TrimSpace(globalLogs) != "" {
		return fmt.Sprintf("Nenhum log de servidor dev disponível para %s.\n\nEventos do DevLAN:\n%s", name, globalLogs), nil
	}
	return fmt.Sprintf("Nenhum log de servidor dev disponível para %s.\nO servidor ainda não foi iniciado ou o projeto não usa o modo dev.", name), nil
}

func (a *App) RunDoctor(name string) ([]DoctorCheckView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.api.BuildDoctorChecksView(ctx, name)
}

func (a *App) ApplyDoctorFix(action, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var err error
	switch action {
	case "reload":
		_, err = a.commands.Reload(ctx)
	case "firewall":
		err = a.commands.ReconcileFirewall(ctx)
	case "topology", "topology-repair":
		_, err = a.commands.RepairM8(ctx)
	case "trust":
		err = a.commands.Trust(ctx)
	case "restart-dev":
		if target != "" {
			err = a.commands.RestartDev(ctx, target)
		}
	default:
		_, err = a.commands.Reload(ctx)
	}
	if err == nil {
		a.api.InvalidateReadModelCache()
	}
	return err
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
	_, err := a.commands.Reload(ctx)
	if err == nil {
		a.api.InvalidateReadModelCache()
	}
	return err
}

func (a *App) ExportConfigJSON() (string, error) {
	data, err := a.queries.ExportConfig()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a *App) ExportDiagnostic() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.commands.DiagnosticBundle(ctx, "")
}

func (a *App) TrustCA() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := a.commands.Trust(ctx)
	if err == nil {
		a.api.InvalidateColdReadModelCache()
	}
	return err
}

func (a *App) GetSecurityAudit(lines int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.queries.SecurityAuditLogs(ctx, lines)
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
