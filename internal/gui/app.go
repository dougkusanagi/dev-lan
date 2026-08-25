package gui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type ProjectView struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	Kind           string `json:"kind"` // "linked" or "parked"
	Mode           string `json:"mode"`
	EffectiveMode  string `json:"effectiveMode"`
	Framework      string `json:"framework"`
	URL            string `json:"url"`
	LANURL         string `json:"lanUrl"`
	TLSEnabled     bool   `json:"tlsEnabled"`
	RoutingMode    string `json:"routingMode"`
	Port           int    `json:"port,omitempty"`
	Host           string `json:"host,omitempty"`
	Status         string `json:"status"` // "ready", "starting", "stopped", "degraded", "error"
	StatusDetail   string `json:"statusDetail,omitempty"`
	PHPVersion     string `json:"phpVersion,omitempty"`
	PackageManager string `json:"packageManager,omitempty"`
	StaticDir      string `json:"staticDir,omitempty"`
	DevRunning     bool   `json:"devRunning"`
	DevPid         int    `json:"devPid,omitempty"`
	DevPort        int    `json:"devPort,omitempty"`
}

type SystemStatusView struct {
	LANIP               string   `json:"lanIp"`
	WindowsPort         int      `json:"windowsPort"`
	HTTPSPort           int      `json:"httpsPort"`
	TLSEnabled          bool     `json:"tlsEnabled"`
	DefaultMode         string   `json:"defaultMode"`
	PHPDefaultVersion   string   `json:"phpDefaultVersion"`
	WindowsCaddyRunning bool     `json:"windowsCaddyRunning"`
	WSLCaddyRunning     bool     `json:"wslCaddyRunning"`
	WSLAvailable        bool     `json:"wslAvailable"`
	FirewallOk          bool     `json:"firewallOk"`
	PHPVersions         []string `json:"phpVersions"`
	TotalProjects       int      `json:"totalProjects"`
}

type DoctorCheckView struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // "OK", "WARN", "FAIL"
	Detail    string `json:"detail"`
	Fixable   bool   `json:"fixable"`
	FixAction string `json:"fixAction,omitempty"`
}

type GlobalConfigView struct {
	DefaultMode       string   `json:"defaultMode"`
	WindowsPort       int      `json:"windowsPort"`
	HTTPSPort         int      `json:"httpsPort"`
	TLSEnabled        bool     `json:"tlsEnabled"`
	PHPDefaultVersion string   `json:"phpDefaultVersion"`
	Allowlist         []string `json:"allowlist"`
	DefaultRouteMode  string   `json:"defaultRouteMode"`
}

type ProjectConfigUpdate struct {
	Name       string `json:"name"`
	Mode       string `json:"mode,omitempty"`
	PHPVersion string `json:"phpVersion,omitempty"`
	PHPPreset  string `json:"phpPreset,omitempty"`
	TLSEnabled *bool  `json:"tlsEnabled,omitempty"`
	RouteMode  string `json:"routeMode,omitempty"`
	RoutePort  int    `json:"routePort,omitempty"`
	RouteHost  string `json:"routeHost,omitempty"`
	StaticDir  string `json:"staticDir,omitempty"`
	DevCommand string `json:"devCommand,omitempty"`
	DevPort    int    `json:"devPort,omitempty"`
}

type App struct {
	service *app.App
	ctx     context.Context
}

func NewApp(service *app.App) *App {
	return &App{service: service}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		return
	}
	go func() {
		startupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = a.service.Reload(startupCtx)
	}()
}

func (a *App) GetProjects(filter string) ([]ProjectView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := a.service.Store.Load()
	if err != nil {
		return nil, err
	}
	effective, err := a.service.EffectiveConfig(ctx, cfg)
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

	filterLower := strings.ToLower(strings.TrimSpace(filter))
	result := make([]ProjectView, 0, len(effective.Projects))

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
		url := resolved.URL(host, cfg.WindowsPort, cfg.HTTPSPort, tlsActive)

		kind := "linked"
		if resolved.Source == domain.SourcePark {
			kind = "parked"
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
			Name:           project.Name,
			Path:           project.Path,
			Kind:           kind,
			Mode:           modeVal,
			EffectiveMode:  string(resolved.Mode),
			Framework:      framework,
			URL:            url,
			LANURL:         url,
			TLSEnabled:     tlsActive,
			RoutingMode:    string(resolved.RouteMode),
			Port:           resolved.RoutePort,
			Host:           resolved.RouteHost,
			Status:         "ready",
			PHPVersion:     phpVer,
			PackageManager: pm,
			StaticDir:      staticDir,
			DevRunning:     false,
		}
		if !edgeReady || !wslReady {
			view.Status = "degraded"
			missing := make([]string, 0, 2)
			if !edgeReady {
				missing = append(missing, "Caddy Windows")
			}
			if !wslReady {
				missing = append(missing, "Caddy WSL")
			}
			view.StatusDetail = "infraestrutura indisponível: " + strings.Join(missing, ", ")
		}
		if resolved.Mode == domain.ModePHP && wslReady && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
			socket := effective.PHPSocket(project)
			socketReady, socketErr := a.service.WSL.IsSocket(ctx, socket)
			if socketErr != nil || !socketReady {
				view.Status = "degraded"
				view.StatusDetail = "socket PHP-FPM indisponível: " + socket
			}
		}

		// Check dev server status if applicable
		if resolved.Mode == domain.ModeDev || resolved.Mode == domain.ModeAuto {
			devStatus, devErr := a.service.DevStatus(ctx, project.Name)
			if devErr == nil {
				view.DevPort = devStatus.Port
				view.DevPid = devStatus.PID
				switch devStatus.State {
				case platform.StateRunning:
					view.DevRunning = true
					if view.Status != "degraded" {
						view.Status = "ready"
					}
				case platform.StateStarting:
					view.DevRunning = true
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

	return result, nil
}

func (a *App) GetStatus() (SystemStatusView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg, err := a.service.Store.Load()
	if err != nil {
		return SystemStatusView{}, err
	}

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
	hasBash, _ := a.service.WSL.HasCommand(ctx, "bash")
	wslAvail := hasBash || a.service.WSLCaddy.Available(ctx) == nil
	firewallOk, _ := platform.FirewallRule(ctx, "DevLAN")

	phpVers, _ := a.service.PHPVersions(ctx)
	vers := make([]string, 0, len(phpVers))
	for _, v := range phpVers {
		vers = append(vers, v.Version)
	}

	return SystemStatusView{
		LANIP:               host,
		WindowsPort:         cfg.WindowsPort,
		HTTPSPort:           cfg.HTTPSPort,
		TLSEnabled:          cfg.TLSEnabled,
		DefaultMode:         string(cfg.DefaultMode),
		PHPDefaultVersion:   cfg.PHPDefaultVersion,
		WindowsCaddyRunning: winCaddyRunning,
		WSLCaddyRunning:     wslCaddyRunning,
		WSLAvailable:        wslAvail,
		FirewallOk:          firewallOk,
		PHPVersions:         vers,
		TotalProjects:       len(cfg.Projects),
	}, nil
}

func (a *App) GetGlobalConfig() (GlobalConfigView, error) {
	cfg, err := a.service.Store.Load()
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
		DefaultRouteMode:  string(cfg.DefaultRouteMode),
	}, nil
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
	if view.DefaultRouteMode != "" {
		rm, err := domain.ParseRouteMode(view.DefaultRouteMode)
		if err != nil {
			return err
		}
		cfg.DefaultRouteMode = rm
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
	if update.RouteMode != "" || update.RoutePort > 0 || update.RouteHost != "" {
		var modePtr *domain.RouteMode
		if update.RouteMode != "" {
			rm, err := domain.ParseRouteMode(update.RouteMode)
			if err != nil {
				return err
			}
			modePtr = &rm
		}
		var portPtr *int
		if update.RoutePort > 0 {
			portPtr = &update.RoutePort
		}
		var hostPtr *string
		if update.RouteHost != "" {
			hostPtr = &update.RouteHost
		}
		if _, err := a.service.SetRouteMode(ctx, update.Name, modePtr, portPtr, hostPtr); err != nil {
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
	// Try project dev logs first
	devLogs, err := a.service.ProjectDevLogs(ctx, name, lines)
	if err == nil && strings.TrimSpace(devLogs) != "" {
		return devLogs, nil
	}
	// A project has a dedicated log only after a dev server has been started.
	// Do not interpret its name as a global component name: that produced the
	// misleading "log não encontrado: <project>" message in the GUI.
	globalLogs, globalErr := a.service.Logs("devlan")
	if globalErr == nil && strings.TrimSpace(globalLogs) != "" {
		return fmt.Sprintf("Nenhum log de servidor dev disponível para %s.\n\nEventos do DevLAN:\n%s", name, globalLogs), nil
	}
	return fmt.Sprintf("Nenhum log de servidor dev disponível para %s.\nO servidor ainda não foi iniciado ou o projeto não usa o modo dev.", name), nil
}

func (a *App) RunDoctor(name string) ([]DoctorCheckView, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	checks, err := a.service.Doctor(ctx, name)
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

func (a *App) ApplyDoctorFix(action, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch action {
	case "reload":
		_, err := a.service.Reload(ctx)
		return err
	case "firewall":
		cfg, err := a.service.Store.Load()
		if err != nil {
			return err
		}
		return platform.EnsureFirewall(ctx, cfg.WindowsPort, cfg.HTTPSPort)
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
