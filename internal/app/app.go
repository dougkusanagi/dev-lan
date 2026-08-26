package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/caddy"
	"github.com/dougkusanagi/dev-lan/internal/config"
	"github.com/dougkusanagi/dev-lan/internal/detect"
	"github.com/dougkusanagi/dev-lan/internal/diagnostic"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	phpconfig "github.com/dougkusanagi/dev-lan/internal/php"
	"github.com/dougkusanagi/dev-lan/internal/platform"
	routealloc "github.com/dougkusanagi/dev-lan/internal/route"
	"github.com/dougkusanagi/dev-lan/internal/telemetry"
)

type App struct {
	Store        config.Store
	Detector     detect.Detector
	WSL          platform.WSLRunner
	PHP          platform.PHPManager
	Dev          platform.DevManager
	DevProxy     *platform.DevProxy
	Telemetry    telemetry.Store
	WindowsCaddy platform.CaddyClient
	WSLCaddy     platform.CaddyClient
	// Firewall accepts both the range-aware FirewallReconciler and the legacy
	// FirewallManager so tests and older integrations can inject either adapter.
	Firewall any
	// ExternalListeners is injectable because a port scan is a host concern,
	// while the allocation policy itself remains pure. Production uses the
	// platform adapter; tests can provide a deterministic snapshot.
	ExternalListeners func(context.Context) ([]int, error)
	Now               func() time.Time
	mutationMu        sync.Mutex
}

type mockRunner struct{}

func (mockRunner) Run(context.Context, ...string) (string, error) { return "", nil }

func New(dataDir string) *App {
	distribution := ""
	if data, err := os.ReadFile(filepath.Join(dataDir, "wsl-distribution")); err == nil {
		distribution = strings.TrimSpace(string(data))
	}
	wsl := platform.NewWSLRunner("wsl.exe", distribution)
	dev := platform.NewWSLDevManager(wsl)
	winCaddy := platform.NewLocalCaddy("")
	wslCaddy := platform.NewWSLCaddy(wsl)
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		winCaddy = platform.CaddyClient{Runner: mockRunner{}}
		wslCaddy = platform.CaddyClient{Runner: mockRunner{}, WSL: true}
	}
	return &App{
		Store:             config.NewStore(dataDir),
		Detector:          detect.Detector{Inspector: detect.SmartInspector{WSL: wsl}},
		WSL:               wsl,
		PHP:               platform.NewWSLPHPManager(wsl),
		Dev:               dev,
		DevProxy:          platform.NewDevProxy(dev),
		Telemetry:         telemetry.NewStore(dataDir),
		WindowsCaddy:      winCaddy,
		WSLCaddy:          wslCaddy,
		Firewall:          platform.SystemFirewall{},
		ExternalListeners: platform.ListeningTCPPorts,
		Now:               time.Now,
	}
}

type ApplyResult struct {
	Warnings []string
	Status   string `json:"status,omitempty"`
	Revision uint64 `json:"revision,omitempty"`
}

type OperationMode string

const (
	BootstrapTolerant OperationMode = "bootstrap-tolerant"
	OperationalStrict OperationMode = "operational-strict"
)

func (a *App) ensureFirewall(ctx context.Context, ports ...int) error {
	if a.Firewall == nil {
		return platform.SystemFirewall{}.Ensure(ctx, ports...)
	}
	manager, ok := a.Firewall.(platform.FirewallManager)
	if !ok {
		return fmt.Errorf("adapter de firewall legado não configurado")
	}
	return manager.Ensure(ctx, ports...)
}

func (a *App) ensureFirewallSpec(ctx context.Context, cfg domain.Config) error {
	spec := platform.FirewallSpecForConfig(cfg)
	if reconciler, ok := a.Firewall.(platform.FirewallReconciler); ok {
		return reconciler.Reconcile(ctx, spec)
	}
	// Keep compatibility with a legacy injected manager while all production
	// paths use the complete range-aware specification.
	ports := append([]int(nil), spec.Ports...)
	for _, portRange := range spec.Ranges {
		for port := portRange.From; port <= portRange.To; port++ {
			ports = append(ports, port)
		}
	}
	return a.ensureFirewall(ctx, ports...)
}

func (a *App) inspectFirewall(ctx context.Context) (platform.FirewallRuleState, error) {
	if reconciler, ok := a.Firewall.(platform.FirewallReconciler); ok {
		return reconciler.Inspect(ctx)
	}
	if a.Firewall == nil {
		return (platform.SystemFirewall{}).Inspect(ctx)
	}
	return platform.FirewallRuleState{}, fmt.Errorf("adapter de firewall não oferece inspeção exata")
}

// FirewallHealthy checks the exact desired policy, including every port
// property, rather than treating the mere presence of a similarly named rule
// as success.
func (a *App) FirewallHealthy(ctx context.Context, cfg domain.Config) (bool, error) {
	rule, err := a.inspectFirewall(ctx)
	if err != nil {
		return false, err
	}
	return rule.Matches(platform.FirewallSpecForConfig(cfg)), nil
}

func (a *App) ReconcileFirewall(ctx context.Context) error {
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	return a.ensureFirewallSpec(ctx, cfg)
}

func firewallSpecDescription(spec platform.FirewallSpec) string {
	parts := make([]string, 0, len(spec.Ports)+len(spec.Ranges))
	for _, port := range spec.Ports {
		parts = append(parts, strconv.Itoa(port))
	}
	for _, portRange := range spec.Ranges {
		parts = append(parts, fmt.Sprintf("%d-%d", portRange.From, portRange.To))
	}
	return strings.Join(parts, ",")
}

// routeAllocationConfig resolves parks and computes a complete, atomic route
// allocation plan. It is called while the Store lock is held by every
// operation that can change or apply routing.
func (a *App) routeAllocationConfig(ctx context.Context, cfg domain.Config) (domain.Config, error) {
	effective, err := a.EffectiveConfig(ctx, cfg)
	if err != nil {
		return domain.Config{}, err
	}
	reserved := []int{cfg.WindowsPort, cfg.HTTPSPort, cfg.WSLPort, cfg.UIPort}
	reservedSet := make(map[int]struct{}, len(reserved))
	for _, port := range reserved {
		reservedSet[port] = struct{}{}
	}
	for _, project := range effective.Projects {
		// Dev gateways and their backend are runtime listeners as well. Reserving
		// both avoids a route being assigned over a JS runtime port.
		devPort := effective.DevPort(project)
		reserved = append(reserved, devPort)
		reservedSet[devPort] = struct{}{}
		backend := devPort + 10000
		if devPort > 55000 {
			backend = devPort - 1000
		}
		if backend > 0 && backend <= 65535 {
			reserved = append(reserved, backend)
			reservedSet[backend] = struct{}{}
		}
	}

	listeners := []int(nil)
	if a.ExternalListeners != nil {
		listeners, err = a.ExternalListeners(ctx)
		if err != nil {
			return domain.Config{}, fmt.Errorf("verificar listeners externos: %w", err)
		}
		filtered := make([]int, 0, len(listeners))
		for _, port := range listeners {
			// Caddy and runtime listeners are expected to be present during a
			// reload. They are already represented in the reservations above.
			if _, managed := reservedSet[port]; managed {
				continue
			}
			if activeRoutePortOwner(effective, port) {
				continue
			}
			filtered = append(filtered, port)
		}
		listeners = filtered
	}
	projects := make([]routealloc.Project, 0, len(effective.Projects))
	for _, project := range effective.Projects {
		projects = append(projects, routealloc.Project{Name: project.Name, Path: project.Path, Override: project.RoutePort})
	}
	plan, err := routealloc.Allocate(routealloc.Input{
		BasePort:          cfg.RouteBasePort,
		PortCount:         cfg.RoutePortCount,
		ReservedPorts:     reserved,
		ExternalListeners: listeners,
		Allocations:       cfg.RoutePortAllocations,
		Projects:          projects,
	})
	if err != nil {
		return domain.Config{}, err
	}
	cfg.RoutePortAllocations = plan.Allocations
	if err := cfg.Normalize(); err != nil {
		return domain.Config{}, err
	}
	return cfg, nil
}

func activeRoutePortOwner(cfg domain.Config, port int) bool {
	for _, project := range cfg.Projects {
		if project.RoutePort != nil && *project.RoutePort == port {
			return true
		}
		if project.RoutePort == nil && cfg.RoutePortAllocations[project.Path] == port {
			return true
		}
	}
	return false
}

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *App) removeFirewall(ctx context.Context) error {
	if a.Firewall == nil {
		return platform.SystemFirewall{}.Remove(ctx)
	}
	if reconciler, ok := a.Firewall.(platform.FirewallReconciler); ok {
		return reconciler.Remove(ctx)
	}
	if manager, ok := a.Firewall.(platform.FirewallManager); ok {
		return manager.Remove(ctx)
	}
	return fmt.Errorf("adapter de firewall não configurado")
}

func (a *App) CloseDevProxies() error {
	if a.DevProxy == nil {
		return nil
	}
	return a.DevProxy.Close()
}

func (a *App) Install(ctx context.Context) (ApplyResult, error) {
	return a.InstallWithOptions(ctx, true)
}

func (a *App) InstallWithOptions(ctx context.Context, configureFirewall bool) (ApplyResult, error) {
	return a.InstallWithPort(ctx, configureFirewall, 0)
}

func (a *App) InstallWithPort(ctx context.Context, configureFirewall bool, windowsPort int) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	if windowsPort != 0 {
		cfg.WindowsPort = windowsPort
	}
	result, err := a.saveAndApplyMode(ctx, cfg, false, BootstrapTolerant)
	if err != nil {
		return result, err
	}
	if !platform.IsAdminResponsive(platform.WindowsCaddyAdminAddress) {
		if !platform.IsPortAvailable(cfg.WindowsPort) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("porta HTTP %d parece estar em uso por outro processo; use `devlan install --windows-port PORT` se houver conflito", cfg.WindowsPort))
		}
		if cfg.TLSEnabled && !platform.IsPortAvailable(cfg.HTTPSPort) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("porta HTTPS %d parece estar em uso por outro processo", cfg.HTTPSPort))
		}
	}
	if configureFirewall {
		if err := a.ensureFirewallSpec(ctx, cfg); err != nil && runtime.GOOS == "windows" {
			result.Warnings = append(result.Warnings, "não foi possível criar a regra de firewall DevLAN; execute install como administrador")
		}
	}
	if err := a.WindowsCaddy.Available(ctx); err != nil {
		result.Warnings = append(result.Warnings, "Caddy Windows não encontrado; instale-o e execute devlan doctor")
	}
	if err := a.WSLCaddy.Available(ctx); err != nil {
		result.Warnings = append(result.Warnings, "Caddy no WSL não encontrado; instale-o e execute devlan doctor")
	}
	phpFound := false
	for _, command := range []string{"php-fpm", "php-fpm8.5", "php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.1"} {
		found, _ := a.WSL.HasCommand(ctx, command)
		if found {
			phpFound = true
			break
		}
	}
	if !phpFound {
		result.Warnings = append(result.Warnings, "PHP-FPM não encontrado no WSL; instale uma versão suportada e execute devlan doctor")
	}
	result.Status = statusFor(result)
	_ = a.appendLog("install concluído")
	a.recordTelemetry("install", map[string]string{"result": "ok"})
	return result, nil
}

func (a *App) Uninstall(ctx context.Context) (ApplyResult, error) {
	result := ApplyResult{}
	_ = a.appendLog("uninstall iniciado")
	if err := a.removeFirewall(ctx); err != nil && runtime.GOOS == "windows" {
		result.Warnings = append(result.Warnings, "não foi possível remover a regra de firewall DevLAN; execute uninstall como administrador")
	}
	if err := a.Store.RemoveManagedFiles(); err != nil {
		return result, err
	}
	a.recordTelemetry("uninstall", map[string]string{"result": "ok"})
	return result, nil
}

func (a *App) Link(ctx context.Context, name, projectPath string) (domain.Project, ApplyResult, error) {
	normalizedPath, err := domain.NormalizePath(projectPath)
	if err != nil {
		return domain.Project{}, ApplyResult{}, err
	}
	detected, err := a.Detector.DetectProject(ctx, normalizedPath)
	if err != nil {
		return domain.Project{}, ApplyResult{}, err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return domain.Project{}, ApplyResult{}, err
	}
	project, err := cfg.Link(name, normalizedPath)
	if err != nil {
		return domain.Project{}, ApplyResult{}, err
	}
	for i := range cfg.Projects {
		if cfg.Projects[i].Name == project.Name {
			switch detected.Kind {
			case detect.ProjectKindPHP:
				preset := detected.PHP.Preset
				cfg.Projects[i].PHPPreset = &preset
				mode := domain.ModePHP
				cfg.Projects[i].Mode = &mode
			case detect.ProjectKindDev:
				mode := domain.ModeDev
				cfg.Projects[i].Mode = &mode
				pm := detected.JS.PackageManager
				cfg.Projects[i].PackageManager = &pm
				fw := detected.JS.Framework
				cfg.Projects[i].DevFramework = &fw
				if detected.JS.DevScript != "" {
					devCmd := detected.JS.DevScript
					cfg.Projects[i].DevCommand = &devCmd
				}
				if detected.JS.StaticDir != "" {
					staticDir := detected.JS.StaticDir
					cfg.Projects[i].StaticDir = &staticDir
				}
				spa := detected.JS.IsSPA
				cfg.Projects[i].SPAFallback = &spa
			case detect.ProjectKindStatic:
				mode := domain.ModeStatic
				cfg.Projects[i].Mode = &mode
				if detected.JS.StaticDir != "" {
					staticDir := detected.JS.StaticDir
					cfg.Projects[i].StaticDir = &staticDir
				}
				spa := detected.JS.IsSPA
				cfg.Projects[i].SPAFallback = &spa
			}
			project = cfg.Projects[i]
			break
		}
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err != nil {
		return domain.Project{}, result, err
	}
	_ = a.appendLog("link %s %s", project.Name, project.Path)
	a.recordTelemetry("link", map[string]string{"mode": string(detected.Kind), "result": "ok"})
	return project, result, nil
}

func (a *App) Unlink(ctx context.Context, name string) (domain.Project, ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return domain.Project{}, ApplyResult{}, err
	}
	project, err := cfg.Unlink(name)
	if err != nil {
		return domain.Project{}, ApplyResult{}, err
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err != nil {
		return domain.Project{}, result, err
	}
	_ = a.appendLog("unlink %s", project.Name)
	a.recordTelemetry("unlink", map[string]string{"result": "ok"})
	return project, result, nil
}

// IgnoreProject hides a project discovered from a parked directory without
// registering it as an explicit project and without touching its files.
func (a *App) IgnoreProject(ctx context.Context, selector string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	effective, err := a.EffectiveConfig(ctx, cfg)
	if err != nil {
		return ApplyResult{}, err
	}
	selected, found := projectBySelector(effective.Projects, strings.TrimSpace(selector))
	if !found {
		return ApplyResult{}, fmt.Errorf("projeto não encontrado: %s", selector)
	}
	if _, linked := cfg.Project(selected.Name); linked {
		return ApplyResult{}, fmt.Errorf("projeto %s está vinculado; use desvincular", selected.Name)
	}
	if err := cfg.IgnoreParkProject(selected.Path); err != nil {
		return ApplyResult{}, err
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err != nil {
		return result, err
	}
	_ = a.appendLog("projeto estacionado ocultado %s", selected.Name)
	return result, nil
}

// UnignoreProject makes a project below a parked directory discoverable again.
func (a *App) UnignoreProject(ctx context.Context, projectPath string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	if err := cfg.UnignoreParkProject(projectPath); err != nil {
		return ApplyResult{}, err
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err != nil {
		return result, err
	}
	_ = a.appendLog("projeto estacionado exibido novamente %s", projectPath)
	return result, nil
}

func (a *App) Park(ctx context.Context, projectPath string) (domain.Park, ApplyResult, error) {
	normalizedPath, err := domain.NormalizePath(projectPath)
	if err != nil {
		return domain.Park{}, ApplyResult{}, err
	}
	isDirectory, err := a.Detector.Inspector.Directory(ctx, normalizedPath)
	if err != nil {
		return domain.Park{}, ApplyResult{}, err
	}
	if !isDirectory {
		return domain.Park{}, ApplyResult{}, fmt.Errorf("diretório não encontrado: %s", normalizedPath)
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return domain.Park{}, ApplyResult{}, err
	}
	park, err := cfg.Park(normalizedPath)
	if err != nil {
		return domain.Park{}, ApplyResult{}, err
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err != nil {
		return domain.Park{}, result, err
	}
	_ = a.appendLog("park %s", park.Path)
	return park, result, nil
}

func (a *App) Unpark(ctx context.Context, projectPath string) (domain.Park, ApplyResult, error) {
	_ = ctx
	cfg, err := a.Store.Load()
	if err != nil {
		return domain.Park{}, ApplyResult{}, err
	}
	park, err := cfg.Unpark(projectPath)
	if err != nil {
		return domain.Park{}, ApplyResult{}, err
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err != nil {
		return domain.Park{}, result, err
	}
	_ = a.appendLog("unpark %s", park.Path)
	return park, result, nil
}

func (a *App) SetDefaultMode(ctx context.Context, mode domain.Mode) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	if err := cfg.SetDefaultMode(mode); err != nil {
		return ApplyResult{}, err
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("modo global %s", mode)
	}
	return result, err
}

func (a *App) SetProjectMode(ctx context.Context, name string, mode *domain.Mode) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	if err := cfg.SetProjectMode(name, mode); err != nil {
		return ApplyResult{}, err
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		if mode == nil {
			_ = a.appendLog("modo do projeto %s herdado", name)
		} else {
			_ = a.appendLog("modo do projeto %s %s", name, *mode)
		}
	}
	return result, err
}

var defaultPHPExtensions = []string{"bcmath", "curl", "gd", "intl", "mbstring", "mysql", "pgsql", "xml", "zip"}

type PHPVersionStatus struct {
	Version    string
	Installed  bool
	Configured bool
	Extensions []string
}

func (a *App) PHPVersions(ctx context.Context) ([]PHPVersionStatus, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	configured := make(map[string]domain.PHPVersionConfig, len(cfg.PHPVersions))
	for _, version := range cfg.PHPVersions {
		configured[version.Version] = version
	}
	installed := map[string]platform.PHPInstallation{}
	if a.PHP != nil {
		items, listErr := a.PHP.List(ctx)
		if listErr != nil && !errors.Is(listErr, platform.ErrUnavailable) {
			return nil, listErr
		}
		for _, item := range items {
			installed[item.Version] = item
		}
	}
	versions := make(map[string]struct{}, len(configured)+len(installed))
	for version := range configured {
		versions[version] = struct{}{}
	}
	for version := range installed {
		versions[version] = struct{}{}
	}
	ordered := make([]string, 0, len(versions))
	for version := range versions {
		ordered = append(ordered, version)
	}
	sort.Strings(ordered)
	result := make([]PHPVersionStatus, 0, len(ordered))
	for _, version := range ordered {
		entry := configured[version]
		extensions := append([]string(nil), entry.Extensions...)
		if len(extensions) == 0 {
			extensions = append(extensions, installed[version].Extensions...)
		}
		sort.Strings(extensions)
		result = append(result, PHPVersionStatus{Version: version, Installed: installed[version].Version != "", Configured: entry.Version != "", Extensions: extensions})
	}
	return result, nil
}

func (a *App) PHPInstall(ctx context.Context, version string, extensions []string) (ApplyResult, error) {
	if a.PHP == nil {
		return ApplyResult{}, fmt.Errorf("gerenciador PHP não configurado")
	}
	version, err := domain.NormalizePHPVersion(version)
	if err != nil {
		return ApplyResult{}, err
	}
	if len(extensions) == 0 {
		extensions = append([]string(nil), defaultPHPExtensions...)
	}
	if err := a.PHP.Install(ctx, version, extensions); err != nil {
		return ApplyResult{}, err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	wasEmpty := len(cfg.PHPVersions) == 0
	if _, found := cfg.PHPVersion(version); found {
		if err := cfg.SetPHPVersionExtensions(version, extensions); err != nil {
			return ApplyResult{}, err
		}
	} else if _, err := cfg.AddPHPVersion(version, extensions); err != nil {
		return ApplyResult{}, err
	}
	if wasEmpty {
		cfg.PHPDefaultVersion = version
		if err := cfg.Normalize(); err != nil {
			return ApplyResult{}, err
		}
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("PHP %s instalado", version)
	}
	return result, err
}

func (a *App) PHPRemove(ctx context.Context, version string) (ApplyResult, error) {
	version, err := domain.NormalizePHPVersion(version)
	if err != nil {
		return ApplyResult{}, err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	if _, found := cfg.PHPVersion(version); !found {
		return ApplyResult{}, fmt.Errorf("versão PHP não registrada: %s", version)
	}
	for _, project := range cfg.Projects {
		if project.PHPVersion != nil && *project.PHPVersion == version {
			return ApplyResult{}, fmt.Errorf("PHP %s ainda é usado pelo projeto %s", version, project.Name)
		}
	}
	result := ApplyResult{}
	if poolManager, ok := a.PHP.(platform.PHPPoolManager); ok {
		if stopErr := poolManager.StopVersion(ctx, version); stopErr != nil && !errors.Is(stopErr, platform.ErrUnavailable) {
			result.Warnings = append(result.Warnings, "não foi possível parar o mestre PHP "+version+": "+stopErr.Error())
		}
	}
	if a.PHP != nil {
		if err := a.PHP.Remove(ctx, version); err != nil {
			return result, err
		}
	}
	if _, err := cfg.RemovePHPVersion(version); err != nil {
		return result, err
	}
	applyResult, err := a.saveAndApply(ctx, cfg, true)
	result.Warnings = append(result.Warnings, applyResult.Warnings...)
	if err == nil {
		_ = a.appendLog("PHP %s removido", version)
	}
	return result, err
}

func (a *App) SetPHPVersionExtensions(ctx context.Context, version string, extensions []string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	if err := cfg.SetPHPVersionExtensions(version, extensions); err != nil {
		return ApplyResult{}, err
	}
	if a.PHP != nil {
		if err := a.PHP.Install(ctx, version, extensions); err != nil {
			return ApplyResult{}, err
		}
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("extensões PHP %s atualizadas", version)
	}
	return result, err
}

func (a *App) SetDefaultPHPVersion(ctx context.Context, version string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	normalized, normalizeErr := domain.NormalizePHPVersion(version)
	if normalizeErr != nil {
		return ApplyResult{}, normalizeErr
	}
	if len(cfg.PHPVersions) == 0 {
		return ApplyResult{}, fmt.Errorf("nenhuma versão PHP foi registrada; use `devlan php install %s`", normalized)
	}
	if _, found := cfg.PHPVersion(normalized); !found {
		return ApplyResult{}, fmt.Errorf("PHP %s não está instalado; use `devlan php install %s`", normalized, normalized)
	}
	if err := cfg.SetDefaultPHPVersion(version); err != nil {
		return ApplyResult{}, err
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("PHP global %s", cfg.PHPDefaultVersion)
	}
	return result, err
}

func (a *App) materializeProject(ctx context.Context, cfg domain.Config, selector string) (domain.Config, string, int, error) {
	effective, err := a.EffectiveConfig(ctx, cfg)
	if err != nil {
		return domain.Config{}, "", -1, err
	}
	selected, found := projectBySelector(effective.Projects, strings.TrimSpace(selector))
	if !found {
		return domain.Config{}, "", -1, fmt.Errorf("projeto não encontrado: %s", selector)
	}
	for i := range cfg.Projects {
		if cfg.Projects[i].Name == selected.Name || cfg.Projects[i].Path == selected.Path {
			return cfg, selected.Name, i, nil
		}
	}
	cfg.Projects = append(cfg.Projects, selected)
	if err := cfg.Normalize(); err != nil {
		return domain.Config{}, "", -1, err
	}
	for i := range cfg.Projects {
		if cfg.Projects[i].Name == selected.Name {
			return cfg, selected.Name, i, nil
		}
	}
	return domain.Config{}, "", -1, fmt.Errorf("projeto materializado desapareceu: %s", selected.Name)
}

func (a *App) SetProjectPHPVersion(ctx context.Context, selector, version string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
	if err != nil {
		return ApplyResult{}, err
	}
	if version == "inherit" {
		cfg.Projects[index].PHPVersion = nil
	} else {
		normalized, normalizeErr := domain.NormalizePHPVersion(version)
		if normalizeErr != nil {
			return ApplyResult{}, normalizeErr
		}
		if len(cfg.PHPVersions) == 0 {
			return ApplyResult{}, fmt.Errorf("nenhuma versão PHP foi registrada; use `devlan php install %s`", normalized)
		}
		if _, found := cfg.PHPVersion(normalized); !found {
			return ApplyResult{}, fmt.Errorf("PHP %s não está instalado; use `devlan php install %s`", normalized, normalized)
		}
		cfg.Projects[index].PHPVersion = &normalized
	}
	if err := cfg.Normalize(); err != nil {
		return ApplyResult{}, err
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("PHP do projeto %s: %s", name, version)
	}
	return result, err
}

func (a *App) SetProjectPHPPreset(ctx context.Context, selector, value string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
	if err != nil {
		return ApplyResult{}, err
	}
	if value == "inherit" {
		cfg.Projects[index].PHPPreset = nil
	} else {
		preset, parseErr := domain.ParsePHPPreset(value)
		if parseErr != nil {
			return ApplyResult{}, parseErr
		}
		cfg.Projects[index].PHPPreset = &preset
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("preset PHP do projeto %s: %s", name, value)
	}
	return result, err
}

func (a *App) SetProjectPHPIsolated(ctx context.Context, selector string, isolated bool) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
	if err != nil {
		return ApplyResult{}, err
	}
	if isolated && len(cfg.PHPVersions) == 0 {
		return ApplyResult{}, fmt.Errorf("pool isolado exige uma versão PHP registrada; use `devlan php install VERSION`")
	}
	cfg.Projects[index].PHPIsolatedPool = &isolated
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("pool PHP do projeto %s: %t", name, isolated)
	}
	return result, err
}

func (a *App) SetPHPGlobalPool(ctx context.Context, pool domain.PHPFPMPoolConfig) (ApplyResult, error) {
	if err := pool.Normalize(); err != nil {
		return ApplyResult{}, err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	cfg.PHPFPMPool = pool
	result, err := a.saveAndApply(ctx, cfg, true)
	return result, err
}

func (a *App) SetPHPVersionPool(ctx context.Context, version string, pool domain.PHPFPMPoolConfig) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	if err := cfg.SetPHPVersionPool(version, pool); err != nil {
		return ApplyResult{}, err
	}
	return a.saveAndApply(ctx, cfg, true)
}

func (a *App) RunComposer(ctx context.Context, selector string, environment string, args []string) (string, error) {
	if a.PHP == nil {
		return "", fmt.Errorf("gerenciador PHP não configurado")
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return "", err
	}
	version, versionErr := domain.NormalizePHPVersion(selector)
	var project *domain.Project
	if versionErr != nil {
		effective, effectiveErr := a.EffectiveConfig(ctx, cfg)
		if effectiveErr != nil {
			return "", effectiveErr
		}
		selected, found := projectBySelector(effective.Projects, selector)
		if !found {
			return "", fmt.Errorf("projeto ou versão PHP não encontrado: %s", selector)
		}
		project = &selected
		version = effective.EffectivePHPVersion(selected)
	}
	if environment == "" {
		environment = string(cfg.Composer.Environment)
		if project != nil && project.ComposerEnvironment != nil {
			environment = string(*project.ComposerEnvironment)
		}
	}
	if len(cfg.PHPVersions) > 0 {
		if _, found := cfg.PHPVersion(version); !found {
			return "", fmt.Errorf("PHP %s não está instalado", version)
		}
	}
	composerBinary := cfg.Composer.Binary
	if configured, found := cfg.PHPVersion(version); found && configured.ComposerBinary != "" {
		composerBinary = configured.ComposerBinary
	}
	manager, ok := a.PHP.(platform.PHPComposerManager)
	if !ok {
		return "", fmt.Errorf("Composer não é suportado pelo gerenciador PHP atual")
	}
	return manager.RunComposer(ctx, version, environment, composerBinary, args...)
}

func (a *App) SetComposerEnvironment(ctx context.Context, selector, value string) (ApplyResult, error) {
	environment, err := domain.ParseComposerEnvironment(value)
	if err != nil {
		return ApplyResult{}, err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	if selector == "default" {
		cfg.Composer.Environment = environment
	} else {
		cfg, _, index, materializeErr := a.materializeProject(ctx, cfg, selector)
		if materializeErr != nil {
			return ApplyResult{}, materializeErr
		}
		cfg.Projects[index].ComposerEnvironment = &environment
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	return result, err
}

func (a *App) PHPInfo(ctx context.Context, selector string) (string, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return "", err
	}
	if selector != "" {
		effective, effectiveErr := a.EffectiveConfig(ctx, cfg)
		if effectiveErr != nil {
			return "", effectiveErr
		}
		selected, found := projectBySelector(effective.Projects, selector)
		if !found {
			return "", fmt.Errorf("projeto não encontrado: %s", selector)
		}
		cfg.Projects = []domain.Project{selected}
	}
	return phpconfig.RenderInfoPage(cfg)
}

func (a *App) SetTLS(ctx context.Context, enabled bool) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	cfg.TLSEnabled = enabled
	result, err := a.saveAndApply(ctx, cfg, true)
	if err != nil {
		return result, err
	}
	if err := a.ensureFirewallSpec(ctx, cfg); err != nil && runtime.GOOS == "windows" {
		result.Warnings = append(result.Warnings, "não foi possível atualizar o firewall; execute o comando em PowerShell como Administrador")
	}
	if enabled {
		if err := a.WindowsCaddy.Trust(ctx); err != nil {
			result.Warnings = append(result.Warnings, "não foi possível confiar na CA local automaticamente; execute `caddy trust` como Administrador")
		}
		_ = a.appendLog("TLS interno ativado")
	} else {
		_ = a.appendLog("TLS interno desativado")
	}
	return result, nil
}

func (a *App) Trust(ctx context.Context) error {
	if a.WindowsCaddy.Runner == nil {
		return fmt.Errorf("Caddy Windows não configurado")
	}
	return a.WindowsCaddy.Trust(ctx)
}

// SetProjectTLS changes the HTTPS preference of one registered project. The
// Windows edge still owns the certificate, but the project selector keeps the
// command and the advertised URL scoped to the requested project.
func (a *App) SetProjectTLS(ctx context.Context, selector string, enabled bool) (ApplyResult, string, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, "", err
	}
	selector = strings.TrimSpace(selector)
	effective, err := a.EffectiveConfig(ctx, cfg)
	if err != nil {
		return ApplyResult{}, "", err
	}
	selected, found := projectBySelector(effective.Projects, selector)
	if !found {
		return ApplyResult{}, "", fmt.Errorf("projeto não encontrado: %s", selector)
	}
	projectIndex := -1
	for i := range cfg.Projects {
		if cfg.Projects[i].Name == selected.Name || cfg.Projects[i].Path == selected.Path {
			projectIndex = i
			break
		}
	}
	if projectIndex < 0 {
		// A parked project is discovered rather than stored. Persist it once its
		// security preference becomes explicit, so the choice survives later
		// commands and does not require a separate `link` step.
		cfg.Projects = append(cfg.Projects, selected)
		projectIndex = len(cfg.Projects) - 1
	}
	cfg.Projects[projectIndex].Secure = &enabled
	if enabled {
		cfg.TLSEnabled = true
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err != nil {
		return result, cfg.Projects[projectIndex].Name, err
	}
	if enabled {
		if err := a.ensureFirewallSpec(ctx, cfg); err != nil && runtime.GOOS == "windows" {
			result.Warnings = append(result.Warnings, "não foi possível atualizar o firewall; execute o comando em PowerShell como Administrador")
		}
		if err := a.WindowsCaddy.Trust(ctx); err != nil {
			result.Warnings = append(result.Warnings, "não foi possível confiar na CA local automaticamente; execute `caddy trust` como Administrador")
		}
	}
	return result, cfg.Projects[projectIndex].Name, nil
}

func projectBySelector(projects []domain.Project, selector string) (domain.Project, bool) {
	for _, project := range projects {
		if project.Name == selector || project.Path == selector {
			return project, true
		}
	}
	if normalized, err := domain.NormalizePath(selector); err == nil {
		for _, project := range projects {
			if project.Path == normalized {
				return project, true
			}
		}
	}
	return domain.Project{}, false
}

func (a *App) Reload(ctx context.Context) (ApplyResult, error) {
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	var result ApplyResult
	err := a.Store.WithLock(ctx, func() error {
		cfg, err := a.Store.LoadLocked()
		if err != nil {
			return err
		}
		prepared, prepareErr := a.routeAllocationConfig(ctx, cfg)
		if prepareErr != nil {
			return prepareErr
		}
		allocationsChanged := !routealloc.EqualAllocations(cfg.RoutePortAllocations, prepared.RoutePortAllocations)
		result, err = a.apply(ctx, prepared, true, false, OperationalStrict)
		if err != nil {
			return err
		}
		if allocationsChanged {
			if err := a.Store.SaveLocked(prepared); err != nil {
				_ = a.Store.RollbackGenerated()
				_ = a.Store.RollbackPHPFiles()
				return err
			}
			result.Revision = cfg.Revision + 1
		}
		result, err = a.reloadApplied(ctx, prepared, result, OperationalStrict)
		if err != nil {
			result.Status = "rolled_back"
			if allocationsChanged {
				_ = a.Store.RollbackConfigLocked()
			}
			_ = a.Store.RollbackGenerated()
			_ = a.Store.RollbackPHPFiles()
			if previous, loadErr := a.Store.LoadLocked(); loadErr == nil {
				_, _ = a.reloadApplied(ctx, previous, ApplyResult{}, BootstrapTolerant)
			}
			return err
		}
		return nil
	})
	if err == nil {
		result.Status = statusFor(result)
		_ = a.appendLog("reload aplicado")
		a.recordTelemetry("reload", map[string]string{"result": "ok"})
	}
	return result, err
}

func (a *App) saveAndApply(ctx context.Context, cfg domain.Config, reload bool) (ApplyResult, error) {
	return a.saveAndApplyMode(ctx, cfg, reload, OperationalStrict)
}

func (a *App) saveAndApplyMode(ctx context.Context, cfg domain.Config, reload bool, mode OperationMode) (ApplyResult, error) {
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	var result ApplyResult
	err := a.Store.WithLock(ctx, func() error {
		current, err := a.Store.LoadLocked()
		if err != nil {
			return err
		}
		if cfg.Revision != 0 && cfg.Revision != current.Revision {
			return fmt.Errorf("%w: esperado %d, atual %d", config.ErrRevisionConflict, cfg.Revision, current.Revision)
		}
		cfg, err = a.routeAllocationConfig(ctx, cfg)
		if err != nil {
			return err
		}
		// Plan, validate and stage happen before the persistent commit. The
		// generated files are backed up by Store and are therefore recoverable
		// if any subsequent phase fails.
		result, err = a.apply(ctx, cfg, true, false, mode)
		if err != nil {
			result.Status = "failed"
			return err
		}
		if err := a.Store.SaveLocked(cfg); err != nil {
			_ = a.Store.RollbackConfigLocked()
			_ = a.Store.RollbackGenerated()
			_ = a.Store.RollbackPHPFiles()
			result.Status = "failed"
			return err
		}
		result.Revision = current.Revision + 1
		if reload {
			result, err = a.reloadApplied(ctx, cfg, result, mode)
			if err != nil {
				// Compensate both files and live processes. A failed post-commit
				// reload must not leave a newer state pointing at older services.
				_ = a.Store.RollbackConfigLocked()
				_ = a.Store.RollbackGenerated()
				_ = a.Store.RollbackPHPFiles()
				if previous, loadErr := a.Store.LoadLocked(); loadErr == nil {
					_, _ = a.reloadApplied(ctx, previous, ApplyResult{}, BootstrapTolerant)
				}
				result.Status = "rolled_back"
				return err
			}
		}
		result.Status = statusFor(result)
		return nil
	})
	if err != nil {
		return result, err
	}
	return result, nil
}

func (a *App) SaveConfigAndApply(ctx context.Context, cfg domain.Config, reload bool) (ApplyResult, error) {
	return a.saveAndApply(ctx, cfg, reload)
}

func statusFor(result ApplyResult) string {
	if len(result.Warnings) > 0 {
		return "degraded"
	}
	return "applied"
}

// reloadApplied is the commit-side runtime phase. It deliberately operates
// only on the already staged/committed artifacts and performs a health check
// after each Caddy operation.
func (a *App) reloadApplied(ctx context.Context, cfg domain.Config, result ApplyResult, mode OperationMode) (ApplyResult, error) {
	if err := a.ensureFirewallSpec(ctx, cfg); err != nil && runtime.GOOS == "windows" {
		result.Warnings = append(result.Warnings, "não foi possível reconciliar o firewall DevLAN: "+err.Error())
	}
	_, phpPools, err := phpconfig.PlansByFile(cfg)
	if err != nil {
		return result, err
	}
	if len(phpPools) > 0 {
		if poolManager, ok := a.PHP.(platform.PHPPoolManager); ok {
			if err := poolManager.EnsurePools(ctx, phpPools); err != nil {
				result.Warnings = append(result.Warnings, "não foi possível iniciar todos os pools PHP: "+err.Error())
			}
		}
	}
	paths := a.Store.Paths()
	if err := a.WindowsCaddy.Available(ctx); err == nil {
		if err := a.WindowsCaddy.EnsureRunning(ctx, paths.WindowsCaddy); err != nil {
			return result, fmt.Errorf("recarregar Caddy Windows: %w", err)
		}
		if err := a.WindowsCaddy.Available(ctx); err != nil {
			return result, fmt.Errorf("healthcheck Caddy Windows: %w", err)
		}
	} else {
		if mode == OperationalStrict {
			return result, fmt.Errorf("Caddy Windows indisponível")
		}
		result.Warnings = append(result.Warnings, "Caddy Windows não disponível; reload ignorado")
	}
	if err := a.WSLCaddy.Available(ctx); err == nil {
		if err := a.WSLCaddy.EnsureRunning(ctx, paths.WSLCaddy); err != nil {
			return result, fmt.Errorf("iniciar/recarregar Caddy WSL: %w", err)
		}
		if err := a.WSLCaddy.Available(ctx); err != nil {
			return result, fmt.Errorf("healthcheck Caddy WSL: %w", err)
		}
	} else {
		if mode == OperationalStrict {
			return result, fmt.Errorf("Caddy no WSL indisponível")
		}
		result.Warnings = append(result.Warnings, "Caddy no WSL não disponível; reload ignorado")
	}
	if a.DevProxy != nil && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		for _, project := range cfg.Projects {
			resolved, resolveErr := cfg.Resolve(project.Name)
			if resolveErr != nil || resolved.Mode != domain.ModeDev {
				continue
			}
			if proxyErr := a.DevProxy.Ensure(ctx, project, cfg.DevPort(project), cfg.DevCommand(project), cfg.ProjectIdleTimeout(project)); proxyErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("proxy dev %s não iniciado: %v", project.Name, proxyErr))
			}
		}
	}
	return result, nil
}

func (a *App) apply(ctx context.Context, cfg domain.Config, validate, reload bool, mode OperationMode) (ApplyResult, error) {
	effective, err := a.EffectiveConfig(ctx, cfg)
	if err != nil {
		return ApplyResult{}, err
	}
	cfg = effective
	if err := a.ensureProjectAccess(ctx, cfg); err != nil {
		return ApplyResult{}, err
	}
	windowsConfig := cfg
	if windowsConfig.TLSEnabled && windowsConfig.LANAddress == "auto" {
		address, addressErr := platform.LANAddress()
		if addressErr != nil {
			return ApplyResult{}, fmt.Errorf("resolver IP LAN para certificado TLS: %w", addressErr)
		}
		windowsConfig.LANAddress = address
	}
	windows, err := caddy.RenderWindows(windowsConfig)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := a.Store.Ensure(); err != nil {
		return ApplyResult{}, err
	}
	accessLogPath := filepath.Join(a.Store.Paths().LogsDir, "access.jsonl")
	wslAccessLogPath, err := platform.ToWSLPath(accessLogPath)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("resolver caminho do access log no WSL: %w", err)
	}
	wsl, err := caddy.RenderWSLWithAccessLog(cfg, wslAccessLogPath)
	if err != nil {
		return ApplyResult{}, err
	}

	result := ApplyResult{}
	phpFiles, phpPools, err := phpconfig.PlansByFile(cfg)
	if err != nil {
		return result, err
	}
	infoPage, err := phpconfig.RenderInfoPage(cfg)
	if err != nil {
		return result, fmt.Errorf("gerar página de informações PHP: %w", err)
	}
	for i := range phpPools {
		phpPools[i].ConfigPath = phpconfig.DisplayPath(a.Store.Paths().PHPGeneratedDir, phpPools[i].Version)
	}
	windowsReady := false
	wslReady := false
	if validate || reload {
		if err := a.WindowsCaddy.Available(ctx); err != nil {
			if mode == OperationalStrict {
				return result, fmt.Errorf("Caddy Windows indisponível: %w", err)
			}
			result.Warnings = append(result.Warnings, "Caddy Windows não disponível; validação/reload externo ignorado")
		} else {
			windowsReady = true
		}
		if err := a.WSLCaddy.Available(ctx); err != nil {
			if mode == OperationalStrict {
				return result, fmt.Errorf("Caddy no WSL indisponível: %w", err)
			}
			result.Warnings = append(result.Warnings, "Caddy no WSL não disponível; validação/reload externo ignorado")
		} else {
			wslReady = true
		}
	}

	validator := func(windowsTemp, wslTemp string) error {
		if validate && windowsReady {
			if err := a.WindowsCaddy.Validate(ctx, windowsTemp); err != nil {
				return fmt.Errorf("Caddy Windows: %w", err)
			}
		}
		if validate && wslReady {
			if err := a.WSLCaddy.Validate(ctx, wslTemp); err != nil {
				return fmt.Errorf("Caddy WSL: %w", err)
			}
		}
		return nil
	}
	var callback func(string, string) error
	if validate {
		callback = validator
	}
	if err := a.Store.ApplyGenerated(windows, wsl, callback); err != nil {
		return result, err
	}
	if err := a.Store.ApplyPHPFiles(phpFiles, infoPage); err != nil {
		_ = a.Store.RollbackGenerated()
		_ = a.Store.RollbackPHPFiles()
		return result, err
	}
	if reload && len(phpPools) > 0 {
		if poolManager, ok := a.PHP.(platform.PHPPoolManager); ok {
			if err := poolManager.EnsurePools(ctx, phpPools); err != nil {
				result.Warnings = append(result.Warnings, "não foi possível iniciar todos os pools PHP: "+err.Error())
			}
		}
	}

	if reload {
		paths := a.Store.Paths()
		if windowsReady {
			if err := a.WindowsCaddy.EnsureRunning(ctx, paths.WindowsCaddy); err != nil {
				_ = a.Store.RollbackGenerated()
				_ = a.Store.RollbackPHPFiles()
				return result, fmt.Errorf("recarregar Caddy Windows: %w", err)
			}
		}
		if wslReady {
			if err := a.WSLCaddy.EnsureRunning(ctx, paths.WSLCaddy); err != nil {
				_ = a.Store.RollbackGenerated()
				_ = a.Store.RollbackPHPFiles()
				return result, fmt.Errorf("iniciar/recarregar Caddy WSL: %w", err)
			}
		}
		if a.DevProxy != nil && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
			for _, project := range cfg.Projects {
				resolved, resolveErr := cfg.Resolve(project.Name)
				if resolveErr != nil || resolved.Mode != domain.ModeDev {
					continue
				}
				if proxyErr := a.DevProxy.Ensure(ctx, project, cfg.DevPort(project), cfg.DevCommand(project), cfg.ProjectIdleTimeout(project)); proxyErr != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("proxy dev %s não iniciado: %v", project.Name, proxyErr))
				}
			}
		}
	}
	return result, nil
}

func (a *App) EffectiveConfig(ctx context.Context, cfg domain.Config) (domain.Config, error) {
	effective := cfg
	knownNames := make(map[string]struct{}, len(cfg.Projects))
	knownPaths := make(map[string]struct{}, len(cfg.Projects))
	for _, project := range cfg.Projects {
		knownNames[project.Name] = struct{}{}
		knownPaths[project.Path] = struct{}{}
	}
	for _, park := range cfg.Parks {
		discovered, err := a.Detector.BatchDiscoverProjects(ctx, park.Path)
		if err != nil {
			if errors.Is(err, platform.ErrUnavailable) {
				continue
			}
			continue
		}
		for _, item := range discovered {
			childPath, err := domain.NormalizePath(item.ProjectPath)
			if err != nil {
				continue
			}
			ignored := false
			for _, ignoredPath := range park.IgnoredPaths {
				if ignoredPath == childPath {
					ignored = true
					break
				}
			}
			if ignored {
				continue
			}
			if _, exists := knownPaths[childPath]; exists {
				continue
			}
			name, err := domain.NormalizeName(pathpkg.Base(childPath))
			if err != nil {
				continue
			}
			if _, exists := knownNames[name]; exists {
				// An explicit link wins over a discovered route with the same
				// stable name.
				continue
			}
			switch item.Kind {
			case detect.ProjectKindPHP:
				preset := item.PHP.Preset
				mode := domain.ModePHP
				effective.Projects = append(effective.Projects, domain.Project{
					Name: name, Path: childPath, Mode: &mode, PHPPreset: &preset,
				})
			case detect.ProjectKindDev:
				mode := domain.ModeDev
				pm := item.JS.PackageManager
				fw := item.JS.Framework
				proj := domain.Project{
					Name: name, Path: childPath, Mode: &mode, PackageManager: &pm, DevFramework: &fw,
				}
				if item.JS.DevScript != "" {
					proj.DevCommand = &item.JS.DevScript
				}
				if item.JS.StaticDir != "" {
					proj.StaticDir = &item.JS.StaticDir
				}
				spa := item.JS.IsSPA
				proj.SPAFallback = &spa
				effective.Projects = append(effective.Projects, proj)
			case detect.ProjectKindStatic:
				mode := domain.ModeStatic
				proj := domain.Project{
					Name: name, Path: childPath, Mode: &mode,
				}
				if item.JS.StaticDir != "" {
					proj.StaticDir = &item.JS.StaticDir
				}
				spa := item.JS.IsSPA
				proj.SPAFallback = &spa
				effective.Projects = append(effective.Projects, proj)
			}
			knownNames[name] = struct{}{}
			knownPaths[childPath] = struct{}{}
		}
	}
	if err := effective.Normalize(); err != nil {
		return domain.Config{}, err
	}
	return effective, nil
}

func (a *App) ensureProjectAccess(ctx context.Context, cfg domain.Config) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	if _, ok := a.Detector.Inspector.(detect.SmartInspector); !ok {
		return nil
	}
	for _, project := range cfg.Projects {
		if err := a.WSL.GrantProjectAccess(ctx, project.Path); err != nil {
			return err
		}
	}
	return nil
}

type Check struct {
	Name   string
	Status string
	Detail string
}

func (a *App) Doctor(ctx context.Context, projectName string) ([]Check, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	checks := []Check{}
	if cfg.LANAddress == "auto" {
		if address, err := platform.LANAddress(); err != nil {
			checks = append(checks, Check{"IP LAN", "WARN", err.Error()})
		} else {
			generated := extractCaddyLANAddress(a.Store.Paths().WindowsCaddy)
			if generated != "" && generated != "localhost" && generated != "127.0.0.1" && address != generated {
				checks = append(checks, Check{"IP LAN", "WARN", fmt.Sprintf("IP atual (%s) diverge do Caddyfile (%s); execute `devlan reload`", address, generated)})
			} else {
				checks = append(checks, Check{"IP LAN", "OK", address})
			}
		}
	} else {
		checks = append(checks, Check{"IP LAN", "OK", cfg.LANAddress + " (configurado)"})
	}

	if err := a.WindowsCaddy.Available(ctx); err != nil {
		checks = append(checks, Check{"Caddy Windows", "WARN", "não encontrado"})
	} else {
		checks = append(checks, Check{"Caddy Windows", "OK", "disponível"})
	}
	if err := a.WSLCaddy.Available(ctx); err != nil {
		checks = append(checks, Check{"Caddy WSL", "WARN", "não encontrado"})
	} else {
		checks = append(checks, Check{"Caddy WSL", "OK", "disponível"})
	}

	// Check Node & JS package managers
	for _, tool := range []string{"node", "npm", "pnpm", "yarn", "bun"} {
		if has, _ := a.WSL.HasCommand(ctx, tool); has {
			checks = append(checks, Check{"WSL " + tool, "OK", "disponível"})
		}
	}

	adminRunning := platform.IsAdminResponsive(platform.WindowsCaddyAdminAddress)
	if adminRunning {
		checks = append(checks, Check{fmt.Sprintf("Porta HTTP (%d)", cfg.WindowsPort), "OK", "gerenciada pelo Caddy Windows"})
		if cfg.TLSEnabled {
			checks = append(checks, Check{fmt.Sprintf("Porta HTTPS (%d)", cfg.HTTPSPort), "OK", "gerenciada pelo Caddy Windows"})
		}
	} else {
		if platform.IsPortAvailable(cfg.WindowsPort) {
			checks = append(checks, Check{fmt.Sprintf("Porta HTTP (%d)", cfg.WindowsPort), "OK", "disponível"})
		} else {
			checks = append(checks, Check{fmt.Sprintf("Porta HTTP (%d)", cfg.WindowsPort), "WARN", "ocupada por outro processo; possível conflito"})
		}
		if cfg.TLSEnabled {
			if platform.IsPortAvailable(cfg.HTTPSPort) {
				checks = append(checks, Check{fmt.Sprintf("Porta HTTPS (%d)", cfg.HTTPSPort), "OK", "disponível"})
			} else {
				checks = append(checks, Check{fmt.Sprintf("Porta HTTPS (%d)", cfg.HTTPSPort), "WARN", "ocupada por outro processo; possível conflito"})
			}
		}
	}

	if len(cfg.PHPVersions) == 0 {
		phpCommands := []string{"php-fpm", "php-fpm8.5", "php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.1"}
		phpFound := false
		for _, command := range phpCommands {
			found, _ := a.WSL.HasCommand(ctx, command)
			if found {
				checks = append(checks, Check{"PHP-FPM", "OK", command})
				phpFound = true
				break
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
		for _, version := range cfg.PHPVersions {
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
			if socket, socketErr := a.WSL.IsSocket(ctx, domain.PHPSharedSocket(version.Version)); socketErr == nil && socket {
				checks = append(checks, Check{"Socket PHP " + version.Version, "OK", domain.PHPSharedSocket(version.Version)})
			} else {
				checks = append(checks, Check{"Socket PHP " + version.Version, "WARN", domain.PHPSharedSocket(version.Version) + " não é socket"})
			}
		}
	}

	if isPublic, netDetail, _ := platform.NetworkProfile(ctx); isPublic {
		checks = append(checks, Check{"Rede Pública", "WARN", netDetail})
	} else {
		checks = append(checks, Check{"Perfil de Rede", "OK", "Privada / confiável"})
	}

	if len(cfg.Allowlist) > 0 {
		checks = append(checks, Check{"Allowlist Global", "OK", strings.Join(cfg.Allowlist, ", ")})
	} else {
		checks = append(checks, Check{"Allowlist Global", "OK", "aberto para sub-rede privada"})
	}

	firewallSpec := platform.FirewallSpecForConfig(cfg)
	if rule, inspectErr := a.inspectFirewall(ctx); inspectErr != nil {
		if errors.Is(inspectErr, platform.ErrFirewallNotFound) {
			checks = append(checks, Check{"Firewall", "WARN", "regra DevLAN ausente; execute `devlan install` ou `devlan reload` como Administrador"})
		} else {
			checks = append(checks, Check{"Firewall", "WARN", "regra DevLAN não confirmada: " + inspectErr.Error()})
		}
	} else if !rule.Matches(firewallSpec) {
		checks = append(checks, Check{"Firewall", "FAIL", "regra DevLAN divergente (direção, ação, protocolo, portas, perfil ou origem); execute `devlan reload` como Administrador"})
	} else {
		checks = append(checks, Check{"Firewall", "OK", "regra DevLAN reconciliada: TCP " + firewallSpecDescription(firewallSpec)})
	}

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

func (a *App) URL(ctx context.Context, projectName string) (string, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return "", err
	}
	resolved, err := cfg.Resolve(projectName)
	if err != nil {
		return "", err
	}
	host := cfg.LANAddress
	if host == "auto" {
		host, err = platform.LANAddress()
		if err != nil {
			host = "localhost"
		}
	}
	return resolved.URL(host, cfg.WindowsPort, cfg.HTTPSPort, cfg.SecureProject(resolved.Project)), nil
}

func (a *App) URLs(ctx context.Context) ([]string, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	host := cfg.LANAddress
	if host == "auto" {
		host, err = platform.LANAddress()
		if err != nil {
			host = "localhost"
		}
	}
	effective, err := a.EffectiveConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resolved, err := effective.ResolvedProjects()
	if err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(resolved))
	for _, item := range resolved {
		urls = append(urls, item.URL(host, cfg.WindowsPort, cfg.HTTPSPort, effective.SecureProject(item.Project)))
	}
	return urls, nil
}

func (a *App) Open(ctx context.Context, projectName string) (string, error) {
	url, err := a.URL(ctx, projectName)
	if err != nil {
		return "", err
	}
	if err := platform.OpenURL(url); err != nil {
		return url, err
	}
	return url, nil
}

func (a *App) Logs(component string) (string, error) {
	paths := a.Store.Paths()
	if component == "" || component == "devlan" {
		data, err := os.ReadFile(filepath.Join(paths.LogsDir, "devlan.log"))
		if errors.Is(err, os.ErrNotExist) {
			return "(nenhum log ainda)\n", nil
		}
		return string(data), err
	}
	if strings.ContainsAny(component, `/\\`) || component == "." || component == ".." {
		return "", fmt.Errorf("componente de log inválido")
	}
	if strings.HasPrefix(component, "php-") {
		version := strings.TrimPrefix(component, "php-")
		if manager, ok := a.PHP.(platform.PHPInfoManager); ok {
			return manager.Logs(context.Background(), version)
		}
	}
	data, err := os.ReadFile(filepath.Join(paths.LogsDir, component+".log"))
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("log não encontrado: %s", component)
	}
	return string(data), err
}

func (a *App) appendLog(format string, values ...any) error {
	if err := a.Store.Ensure(); err != nil {
		return err
	}
	stamp := a.now().Format(time.RFC3339)
	line := fmt.Sprintf("%s %s\n", stamp, fmt.Sprintf(format, values...))
	file, err := os.OpenFile(filepath.Join(a.Store.Paths().LogsDir, "devlan.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(line)
	return err
}

func RuntimeDescription() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}

func extractCaddyLANAddress(caddyfilePath string) string {
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`default_sni\s+([^\s\r\n]+)`)
	matches := re.FindStringSubmatch(string(data))
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (a *App) CheckLANAddressDivergence() (current string, generated string, diverged bool) {
	cfg, err := a.Store.Load()
	if err != nil || cfg.LANAddress != "auto" {
		return "", "", false
	}
	current, err = platform.LANAddress()
	if err != nil {
		return "", "", false
	}
	generated = extractCaddyLANAddress(a.Store.Paths().WindowsCaddy)
	if generated != "" && generated != "localhost" && generated != "127.0.0.1" && current != generated {
		return current, generated, true
	}
	return current, generated, false
}

func (a *App) resolveProject(ctx context.Context, selector string) (domain.Project, domain.Config, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return domain.Project{}, domain.Config{}, err
	}
	effective, err := a.EffectiveConfig(ctx, cfg)
	if err != nil {
		return domain.Project{}, domain.Config{}, err
	}
	project, found := projectBySelector(effective.Projects, selector)
	if !found {
		return domain.Project{}, domain.Config{}, fmt.Errorf("projeto não encontrado: %s", selector)
	}
	return project, effective, nil
}

func (a *App) StartDev(ctx context.Context, selector string) error {
	if a.Dev == nil {
		return fmt.Errorf("gerenciador dev não configurado")
	}
	project, cfg, err := a.resolveProject(ctx, selector)
	if err != nil {
		return err
	}
	resolved, err := cfg.Resolve(project.Name)
	if err != nil {
		return err
	}
	if resolved.Mode != domain.ModeDev && resolved.Mode != domain.ModeAuto && !isLaravelDevScript(cfg, project) {
		return fmt.Errorf("o projeto %s usa o modo %s e não possui servidor dev", project.Name, resolved.Mode)
	}
	port := cfg.DevPort(project)
	cmd := cfg.DevCommand(project)
	var startErr error
	if a.DevProxy != nil && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		startErr = a.DevProxy.StartNow(ctx, project, port, cmd, cfg.ProjectIdleTimeout(project))
	} else {
		startErr = a.Dev.StartDev(ctx, project, port, cmd)
	}
	if startErr != nil {
		_ = a.appendLog("dev start %s falhou: %v", project.Name, startErr)
		return startErr
	}
	_ = a.appendLog("dev start %s (porta %d)", project.Name, port)
	return nil
}

// Laravel projects commonly serve PHP through FPM while their Vite assets run
// through `npm run dev`. Keep that asset process available without changing
// the project's PHP routing mode.
func isLaravelDevScript(cfg domain.Config, project domain.Project) bool {
	return cfg.PHPProjectPreset(project) == domain.PHPPresetLaravel
}

func (a *App) StopDev(ctx context.Context, selector string) error {
	if a.Dev == nil {
		return fmt.Errorf("gerenciador dev não configurado")
	}
	project, cfg, err := a.resolveProject(ctx, selector)
	if err != nil {
		return err
	}
	port := cfg.DevPort(project)
	var stopErr error
	if a.DevProxy != nil && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		stopErr = a.DevProxy.StopProject(ctx, project, port)
	} else {
		stopErr = a.Dev.StopDev(ctx, project, port)
	}
	if stopErr != nil {
		return stopErr
	}
	_ = a.appendLog("dev stop %s", project.Name)
	return nil
}

func (a *App) RestartDev(ctx context.Context, selector string) error {
	if a.Dev == nil {
		return fmt.Errorf("gerenciador dev não configurado")
	}
	project, cfg, err := a.resolveProject(ctx, selector)
	if err != nil {
		return err
	}
	port := cfg.DevPort(project)
	cmd := cfg.DevCommand(project)
	if a.DevProxy != nil && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		if err := a.DevProxy.StopProject(ctx, project, port); err != nil {
			return err
		}
		if err := a.DevProxy.StartNow(ctx, project, port, cmd, cfg.ProjectIdleTimeout(project)); err != nil {
			return err
		}
	} else if err := a.Dev.RestartDev(ctx, project, port, cmd); err != nil {
		return err
	}
	_ = a.appendLog("dev restart %s (porta %d)", project.Name, port)
	return nil
}

func (a *App) BuildProject(ctx context.Context, selector string) (string, error) {
	if a.Dev == nil {
		return "", fmt.Errorf("gerenciador dev não configurado")
	}
	project, cfg, err := a.resolveProject(ctx, selector)
	if err != nil {
		return "", err
	}
	resolved, err := cfg.Resolve(project.Name)
	if err != nil {
		return "", err
	}
	// A production/LAN preview must consume the compiled manifest, never the
	// Vite hot file. Stop the local HMR process first; StopDev also removes
	// public/hot, including a process started by the CLI in another session.
	if resolved.Mode == domain.ModeDev || isLaravelDevScript(cfg, project) {
		if err := a.StopDev(ctx, project.Name); err != nil {
			return "", fmt.Errorf("preparar preview LAN: %w", err)
		}
	}
	pm := cfg.PackageManager(project)
	out, err := a.Dev.Build(ctx, project, pm)
	if err == nil {
		_ = a.appendLog("build %s (%s)", project.Name, pm)
	}
	return out, err
}

func (a *App) InstallDeps(ctx context.Context, selector string) (string, error) {
	project, cfg, err := a.resolveProject(ctx, selector)
	if err != nil {
		return "", err
	}
	outputs := make([]string, 0, 2)
	if a.projectHasManifest(ctx, project, "package.json") {
		if a.Dev == nil {
			return "", fmt.Errorf("gerenciador dev não configurado")
		}
		pm := cfg.PackageManager(project)
		out, installErr := a.Dev.InstallDeps(ctx, project, pm)
		outputs = append(outputs, out)
		if installErr != nil {
			return strings.Join(outputs, "\n"), installErr
		}
		_ = a.appendLog("deps install %s (%s)", project.Name, pm)
	}
	if a.projectHasManifest(ctx, project, "composer.json") {
		out, installErr := a.RunComposer(ctx, project.Name, "", []string{"--working-dir=" + project.Path, "install", "--no-interaction"})
		outputs = append(outputs, out)
		if installErr != nil {
			return strings.Join(outputs, "\n"), installErr
		}
		_ = a.appendLog("deps install %s (composer)", project.Name)
	}
	if len(outputs) == 0 {
		return "", fmt.Errorf("nenhum package.json ou composer.json encontrado em %s", project.Name)
	}
	return strings.Join(outputs, "\n"), nil
}

func (a *App) projectHasManifest(ctx context.Context, project domain.Project, name string) bool {
	if _, err := os.Stat(filepath.Join(filepath.FromSlash(project.Path), name)); err == nil {
		return true
	}
	_, err := a.WSL.Run(ctx, "/bin/sh", "-c", `test -f "$1/$2"`, "devlan", project.Path, name)
	return err == nil
}

func (a *App) ProjectDevLogs(ctx context.Context, selector string, lines int) (string, error) {
	if a.Dev == nil {
		return "", fmt.Errorf("gerenciador dev não configurado")
	}
	project, _, err := a.resolveProject(ctx, selector)
	if err != nil {
		return "", err
	}
	return a.Dev.Logs(ctx, project, lines)
}

func (a *App) DevStatus(ctx context.Context, selector string) (platform.DevProcessStatus, error) {
	if a.Dev == nil {
		return platform.DevProcessStatus{}, fmt.Errorf("gerenciador dev não configurado")
	}
	project, cfg, err := a.resolveProject(ctx, selector)
	if err != nil {
		return platform.DevProcessStatus{}, err
	}
	port := cfg.DevPort(project)
	if a.DevProxy != nil && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		running, starting := a.DevProxy.Status(project.Name)
		state := platform.StateStopped
		if starting {
			state = platform.StateStarting
		} else if running {
			state = platform.StateRunning
		}
		return platform.DevProcessStatus{ProjectName: project.Name, Port: port, State: state}, nil
	}
	return a.Dev.Status(ctx, project, port)
}

func (a *App) SetProjectStaticDir(ctx context.Context, selector, staticDir string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
	if err != nil {
		return ApplyResult{}, err
	}
	var val *string
	if staticDir != "" && staticDir != "inherit" {
		val = &staticDir
	}
	cfg.Projects[index].StaticDir = val
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("static_dir %s %s", name, staticDir)
	}
	return result, err
}

func (a *App) SetProjectDevPort(ctx context.Context, selector string, port int) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
	if err != nil {
		return ApplyResult{}, err
	}
	var val *int
	if port > 0 {
		val = &port
	}
	cfg.Projects[index].DevPort = val
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("dev_port %s %d", name, port)
	}
	return result, err
}

func (a *App) SetProjectDevCommand(ctx context.Context, selector, devCmd string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
	if err != nil {
		return ApplyResult{}, err
	}
	var val *string
	if devCmd != "" && devCmd != "inherit" {
		val = &devCmd
	}
	cfg.Projects[index].DevCommand = val
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("dev_command %s %s", name, devCmd)
	}
	return result, err
}

func (a *App) SetProjectPackageManager(ctx context.Context, selector, pm string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
	if err != nil {
		return ApplyResult{}, err
	}
	var val *string
	if pm != "" && pm != "inherit" {
		val = &pm
	}
	cfg.Projects[index].PackageManager = val
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("package_manager %s %s", name, pm)
	}
	return result, err
}

func (a *App) SetRoutePort(ctx context.Context, selector string, port *int) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	cfg, name, _, err := a.materializeProject(ctx, cfg, selector)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := cfg.SetProjectRoutePort(name, port); err != nil {
		return ApplyResult{}, err
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("porta LAN %s: port=%v", name, port)
		_ = a.Store.AppendSecurityAudit("ROUTE_PORT_CHANGE", fmt.Sprintf("project=%s port=%v", name, port))
	}
	return result, err
}

type RouteAllocation struct {
	Path   string `json:"path"`
	Port   int    `json:"port"`
	Orphan bool   `json:"orphan"`
}

// RouteAllocations returns the persisted automatic assignments without
// triggering discovery or changing state. An orphan is merely reported; it
// remains reserved until the explicit prune command is used.
func (a *App) RouteAllocations(ctx context.Context) ([]RouteAllocation, error) {
	_ = ctx
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	linked := make([]string, 0, len(cfg.Projects))
	for _, project := range cfg.Projects {
		linked = append(linked, project.Path)
	}
	parks := make([]string, 0, len(cfg.Parks))
	for _, park := range cfg.Parks {
		parks = append(parks, park.Path)
	}
	orphanPaths, err := routealloc.OrphanPaths(cfg.RoutePortAllocations, linked, parks)
	if err != nil {
		return nil, err
	}
	orphans := make(map[string]struct{}, len(orphanPaths))
	for _, path := range orphanPaths {
		orphans[path] = struct{}{}
	}
	paths := make([]string, 0, len(cfg.RoutePortAllocations))
	for path := range cfg.RoutePortAllocations {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := make([]RouteAllocation, 0, len(paths))
	for _, path := range paths {
		_, orphan := orphans[path]
		result = append(result, RouteAllocation{Path: path, Port: cfg.RoutePortAllocations[path], Orphan: orphan})
	}
	return result, nil
}

// PruneRouteAllocations removes only allocations that are no longer linked
// and no longer belong to an active park. dryRun never writes state or
// generated files and is safe to use from doctor/UI previews.
func (a *App) PruneRouteAllocations(ctx context.Context, dryRun bool) ([]string, ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, ApplyResult{}, err
	}
	linked := make([]string, 0, len(cfg.Projects))
	for _, project := range cfg.Projects {
		linked = append(linked, project.Path)
	}
	parks := make([]string, 0, len(cfg.Parks))
	for _, park := range cfg.Parks {
		parks = append(parks, park.Path)
	}
	orphanPaths, err := routealloc.OrphanPaths(cfg.RoutePortAllocations, linked, parks)
	if err != nil {
		return nil, ApplyResult{}, err
	}
	if dryRun || len(orphanPaths) == 0 {
		return orphanPaths, ApplyResult{Status: "preview"}, nil
	}
	for _, path := range orphanPaths {
		delete(cfg.RoutePortAllocations, path)
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("alocações de rota órfãs removidas: %d", len(orphanPaths))
		_ = a.Store.AppendSecurityAudit("ROUTE_ALLOCATIONS_PRUNE", fmt.Sprintf("count=%d paths=%v", len(orphanPaths), orphanPaths))
	}
	return orphanPaths, result, err
}

func (a *App) SetAllowlist(ctx context.Context, selector string, cidrs []string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	if selector == "default" || selector == "" {
		if err := cfg.SetGlobalAllowlist(cidrs); err != nil {
			return ApplyResult{}, err
		}
	} else {
		cfg, name, _, err := a.materializeProject(ctx, cfg, selector)
		if err != nil {
			return ApplyResult{}, err
		}
		if err := cfg.SetProjectAllowlist(name, cidrs); err != nil {
			return ApplyResult{}, err
		}
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("allowlist %s atualizada (%d CIDRs)", selector, len(cidrs))
		_ = a.Store.AppendSecurityAudit("ALLOWLIST_SET", fmt.Sprintf("target=%s cidrs=%v", selector, cidrs))
	}
	return result, err
}

func (a *App) AddAllowlist(ctx context.Context, selector string, cidrs []string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	var current []string
	if selector == "default" || selector == "" {
		current = append([]string(nil), cfg.Allowlist...)
	} else {
		cfg, name, idx, err := a.materializeProject(ctx, cfg, selector)
		if err != nil {
			return ApplyResult{}, err
		}
		current = append([]string(nil), cfg.Projects[idx].Allowlist...)
		selector = name
	}
	current = append(current, cidrs...)
	return a.SetAllowlist(ctx, selector, current)
}

func (a *App) RemoveAllowlist(ctx context.Context, selector string, cidrs []string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	toRemove := map[string]bool{}
	for _, c := range cidrs {
		norm, _ := domain.NormalizeCIDR(c)
		if norm != "" {
			toRemove[norm] = true
		}
		toRemove[c] = true
	}
	var current []string
	if selector == "default" || selector == "" {
		current = cfg.Allowlist
	} else {
		cfg, name, idx, err := a.materializeProject(ctx, cfg, selector)
		if err != nil {
			return ApplyResult{}, err
		}
		current = cfg.Projects[idx].Allowlist
		selector = name
	}
	var filtered []string
	for _, item := range current {
		if !toRemove[item] {
			filtered = append(filtered, item)
		}
	}
	return a.SetAllowlist(ctx, selector, filtered)
}

func (a *App) ClearAllowlist(ctx context.Context, selector string) (ApplyResult, error) {
	return a.SetAllowlist(ctx, selector, []string{})
}

func (a *App) ExposeProject(ctx context.Context, selector string, duration time.Duration) (ApplyResult, string, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, "", err
	}
	cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
	if err != nil {
		return ApplyResult{}, "", err
	}
	var untilStr *string
	if duration > 0 {
		exp := a.now().Add(duration).UTC().Format(time.RFC3339)
		untilStr = &exp
	}
	cfg.Projects[index].ExposedUntil = untilStr
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("expose %s duration=%v", name, duration)
		_ = a.Store.AppendSecurityAudit("EXPOSE_PROJECT", fmt.Sprintf("project=%s duration=%v until=%v", name, duration, untilStr))
	}
	return result, name, err
}

func (a *App) UnexposeProject(ctx context.Context, selector string) (ApplyResult, string, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, "", err
	}
	cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
	if err != nil {
		return ApplyResult{}, "", err
	}
	past := a.now().Add(-1 * time.Minute).UTC().Format(time.RFC3339)
	cfg.Projects[index].ExposedUntil = &past
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("unexpose %s", name)
		_ = a.Store.AppendSecurityAudit("UNEXPOSE_PROJECT", fmt.Sprintf("project=%s", name))
	}
	return result, name, err
}

func (a *App) SetAuth(ctx context.Context, selector string, enabled bool, username, password string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	var hash string
	if password != "" {
		if a.WindowsCaddy.Runner != nil {
			h, err := a.WindowsCaddy.HashPassword(ctx, password)
			if err == nil && h != "" {
				hash = strings.TrimSpace(h)
			}
		}
		if hash == "" {
			hash = password
		}
	}
	user := domain.AuthUser{Username: username, PasswordHash: hash}
	if selector == "default" || selector == "" {
		if username != "" {
			cfg.AuthUsers = []domain.AuthUser{user}
		}
	} else {
		cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
		if err != nil {
			return ApplyResult{}, err
		}
		cfg.Projects[index].AuthEnabled = &enabled
		if username != "" {
			cfg.Projects[index].AuthUsers = []domain.AuthUser{user}
		}
		selector = name
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("auth %s enabled=%t user=%s", selector, enabled, username)
		_ = a.Store.AppendSecurityAudit("AUTH_SET", fmt.Sprintf("target=%s enabled=%t user=%s", selector, enabled, username))
	}
	return result, err
}

func (a *App) DisableAuth(ctx context.Context, selector string) (ApplyResult, error) {
	disabled := false
	return a.SetAuth(ctx, selector, disabled, "", "")
}

func (a *App) CAInfo(ctx context.Context) (map[string]string, error) {
	path := platform.FindCARootCertPath()
	info := map[string]string{
		"path": path,
	}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			info["exists"] = "true"
			info["size"] = fmt.Sprintf("%d bytes", len(data))
		} else {
			info["exists"] = "false"
		}
	} else {
		info["exists"] = "false"
	}
	return info, nil
}

func (a *App) ExportCA(ctx context.Context, targetPath string) (string, error) {
	src := platform.FindCARootCertPath()
	if src == "" {
		return "", fmt.Errorf("certificado raiz da CA do Caddy não encontrado no sistema")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("ler CA raiz (%s): %w", src, err)
	}
	if targetPath == "" {
		targetPath = a.Store.Paths().CARootExport
	}
	if err := os.WriteFile(targetPath, data, 0o644); err != nil {
		return "", fmt.Errorf("gravar certificado em %s: %w", targetPath, err)
	}
	_ = a.Store.AppendSecurityAudit("CA_EXPORT", fmt.Sprintf("target=%s", targetPath))
	return targetPath, nil
}

func (a *App) RotateCA(ctx context.Context) (ApplyResult, error) {
	result := ApplyResult{}
	if a.WindowsCaddy.Runner != nil {
		_ = a.WindowsCaddy.Trust(ctx)
	}
	_ = a.Store.AppendSecurityAudit("CA_ROTATE", "rotação de CA solicitada")
	reloadResult, err := a.Reload(ctx)
	result.Warnings = append(result.Warnings, reloadResult.Warnings...)
	return result, err
}

func (a *App) SecurityAuditLogs(ctx context.Context, lines int) (string, error) {
	return a.Store.ReadSecurityAudit(lines)
}

func (a *App) recordTelemetry(name string, attributes map[string]string) {
	_ = a.Telemetry.Record(name, attributes)
}

// ExportConfig returns the portable configuration envelope. It deliberately
// excludes authentication hashes and expiring exposure state.
func (a *App) ExportConfig() ([]byte, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	return config.MarshalExport(cfg)
}

// ImportConfig validates a portable configuration before applying it. The
// generated files are validated/reloaded first; the persisted state changes
// only after the new runtime configuration is accepted.
func (a *App) ImportConfig(ctx context.Context, data []byte) (ApplyResult, error) {
	cfg, err := config.UnmarshalExport(data)
	if err != nil {
		return ApplyResult{}, err
	}
	result, err := a.SaveConfigAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("configuração importada")
		_ = a.Store.AppendSecurityAudit("CONFIG_IMPORT", "configuração portátil importada sem credenciais")
	}
	return result, err
}

// DiagnosticBundle creates a support artifact from an explicit allowlist of
// managed files. Project contents, environment variables and credentials are
// never traversed or included.
func (a *App) DiagnosticBundle(ctx context.Context, targetPath string) (string, error) {
	now := a.now()
	if a.Now != nil {
		now = a.Now()
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return "", err
	}
	exported, err := config.MarshalExport(cfg)
	if err != nil {
		return "", err
	}
	checks, doctorErr := a.Doctor(ctx, "")
	if doctorErr != nil {
		checks = []Check{{Name: "doctor", Status: "WARN", Detail: doctorErr.Error()}}
	}
	doctorData, err := json.MarshalIndent(checks, "", "  ")
	if err != nil {
		return "", fmt.Errorf("serializar diagnóstico: %w", err)
	}

	entries := map[string][]byte{
		"config.json": exported,
		"doctor.json": append(doctorData, '\n'),
		"runtime.txt": []byte(fmt.Sprintf("runtime=%s\ndata_dir=%s\n", RuntimeDescription(), a.Store.Paths().Dir)),
	}
	paths := a.Store.Paths()
	for archiveName, sourcePath := range map[string]string{
		"generated/Caddyfile.windows": paths.WindowsCaddy,
		"generated/Caddyfile.wsl":     paths.WSLCaddy,
		"logs/devlan.log":             filepath.Join(paths.LogsDir, "devlan.log"),
		"logs/security.log":           paths.SecurityLog,
	} {
		data, readErr := os.ReadFile(sourcePath)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return "", fmt.Errorf("ler %s para o diagnóstico: %w", sourcePath, readErr)
		}
		if strings.HasSuffix(archiveName, "Caddyfile.windows") || strings.HasSuffix(archiveName, "Caddyfile.wsl") {
			data = redactDiagnosticConfig(data)
		}
		entries[archiveName] = data
	}

	if targetPath == "" {
		stamp := now.UTC().Format("20060102-150405")
		targetPath = filepath.Join(paths.Dir, "devlan-diagnostic-"+stamp+".zip")
	}
	manifest := diagnostic.Manifest{
		Format:    diagnostic.Format,
		Version:   diagnostic.Version,
		CreatedAt: now.UTC().Format(time.RFC3339),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
	if err := diagnostic.Write(targetPath, manifest, entries); err != nil {
		return "", err
	}
	_ = a.appendLog("diagnóstico exportado: %s", targetPath)
	return targetPath, nil
}

func redactDiagnosticConfig(data []byte) []byte {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	inBasicAuth := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "basicauth {" {
			inBasicAuth = true
			continue
		}
		if inBasicAuth {
			if trimmed == "}" {
				inBasicAuth = false
				continue
			}
			lines[index] = "            <credencial removida do diagnóstico>"
		}
	}
	return []byte(strings.Join(lines, "\n"))
}
