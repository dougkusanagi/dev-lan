package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a
// plaintext credential to be stored as a fallback.
var ErrPasswordHashUnavailable = errors.New("não foi possível gerar o hash da senha; credencial não persistida")

type App struct {
	Store     config.Store
	Detector  detect.Detector
	WSL       platform.WSLRunner
	PHP       platform.PHPManager
	Dev       platform.DevManager
	DevProxy  *platform.DevProxy
	Telemetry telemetry.Store
	// Caddy is the M8 unified edge. It is intentionally optional in the struct
	// so callers compiled against the pre-M8 fields can still inject a WSL
	// client while migrating.
	Caddy        platform.CaddyClient
	WindowsCaddy platform.CaddyClient
	WSLCaddy     platform.CaddyClient
	// Firewall is the small compatibility port. Range-aware implementations may
	// additionally implement platform.FirewallReconciler.
	Firewall platform.FirewallManager
	// ExternalListeners is injectable because a port scan is a host concern,
	// while the allocation policy itself remains pure. Production uses the
	// platform adapter; tests can provide a deterministic snapshot.
	ExternalListeners func(context.Context) ([]int, error)
	Now               func() time.Time
	// WSLConfigPath is injectable for migration tests; empty means the user's
	// host-level .wslconfig.
	WSLConfigPath        string
	mutationMu           sync.Mutex
	topologyMu           sync.Mutex
	operationMu          sync.Mutex
	operations           map[string]OperationState
	operationSubscribers map[*operationSubscriber]struct{}
}

func (a *App) edgeCaddy() platform.CaddyClient {
	// Caddy is the canonical M8 edge. A non-systemd WSLCaddy is accepted only
	// as an explicit compatibility injection from pre-M8 callers/tests; the
	// production constructor leaves it empty and a second systemd edge can
	// never shadow the canonical one.
	if a.Caddy.Runner != nil {
		if a.WSLCaddy.Runner != nil && a.Caddy.RequireSystemd && !a.WSLCaddy.RequireSystemd {
			return a.WSLCaddy
		}
		return a.Caddy
	}
	if a.WSLCaddy.Runner != nil {
		return a.WSLCaddy
	}
	// Compatibility for callers/tests that only injected the old host edge.
	return a.WindowsCaddy
}

type mockRunner struct{}

func (mockRunner) Run(context.Context, ...string) (string, error) {
	// Returning a non-secret sentinel also lets auth characterization tests
	// exercise the successful hash path without starting Caddy.
	return "$2a$10$devlan-test-hash", nil
}

// mockFirewall is selected only by DEVLAN_TEST_MOCK. Keeping this adapter in
// the composition root prevents unit tests from invoking netsh or PowerShell,
// which otherwise causes an interactive Windows Defender Firewall prompt.
type mockFirewall struct{}

func (mockFirewall) Ensure(context.Context, ...int) error { return nil }
func (mockFirewall) Remove(context.Context) error         { return nil }

func New(dataDir string) *App {
	distribution := ""
	if data, err := os.ReadFile(filepath.Join(dataDir, "wsl-distribution")); err == nil {
		distribution = strings.TrimSpace(string(data))
	}
	wsl := platform.NewWSLRunner("wsl.exe", distribution)
	dev := platform.NewWSLDevManager(wsl)
	wslCaddy := platform.NewWSLCaddy(wsl)
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		wslCaddy = platform.CaddyClient{Runner: mockRunner{}, WSL: true}
	}
	var firewall platform.FirewallManager = platform.CompositeFirewall{}
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		firewall = mockFirewall{}
	}
	return &App{
		Store:     config.NewStore(dataDir),
		Detector:  detect.Detector{Inspector: detect.SmartInspector{WSL: wsl}},
		WSL:       wsl,
		PHP:       platform.NewWSLPHPManager(wsl),
		Dev:       dev,
		DevProxy:  platform.NewDevProxy(dev),
		Telemetry: telemetry.NewStore(dataDir),
		// This is the only operational edge. It is deliberately assigned to the
		// canonical field; WSLCaddy below is left nil so a second Caddy cannot be
		// selected accidentally by normal code.
		Caddy: wslCaddy,
		// Windows no longer owns an operational Caddy instance. Keep the field
		// zero-valued so old integrations can still inject a legacy client when
		// explicitly testing or rolling back a migration.
		WindowsCaddy:      platform.CaddyClient{},
		WSLCaddy:          platform.CaddyClient{},
		Firewall:          firewall,
		ExternalListeners: platform.ListeningTCPPorts,
		Now:               time.Now,
		WSLConfigPath:     platform.UserWSLConfigPath(),
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
	return a.Firewall.Ensure(ctx, ports...)
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
	// M8 has no intermediate Windows/WSL listener. Keep the actual edge ports
	// reserved even when an older config still contains legacy port fields.
	reserved := []int{80, 443, cfg.UIPort}
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
	if a.ExternalListeners != nil && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
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
	ctx = platform.WithWSLOperation(ctx, platform.WSLOperationInstall)
	if err := a.ensureInstallationManifest(ctx); err != nil {
		return ApplyResult{}, err
	}
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
	// Caddy may have created its WSL provenance marker while applying the
	// generated config. Refresh the manifest after that side effect as well as
	// before the transaction, so a direct `devlan install` is uninstallable.
	if manifestErr := a.ensureInstallationManifest(ctx); manifestErr != nil {
		result.Warnings = append(result.Warnings, "não foi possível atualizar o manifesto de instalação: "+manifestErr.Error())
	}
	if fingerprintErr := a.refreshWSLManagedFingerprints(ctx, "wsl.caddy-config"); fingerprintErr != nil {
		result.Warnings = append(result.Warnings, "não foi possível atualizar a fingerprint do Caddy WSL: "+fingerprintErr.Error())
	}
	if configureFirewall {
		if err := a.ensureFirewallSpec(ctx, cfg); err != nil && runtime.GOOS == "windows" {
			result.Warnings = append(result.Warnings, "não foi possível reconciliar Windows Firewall/Hyper-V Firewall; execute install como administrador")
		}
	}
	if err := a.edgeCaddy().Available(ctx); err != nil {
		result.Warnings = append(result.Warnings, "Caddy único no WSL não encontrado; instale-o e execute devlan doctor")
	}
	phpCommands := []string{"php-fpm", "php-fpm8.5", "php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.1"}
	phpFound := false
	if found, findErr := a.WSL.HasCommands(ctx, phpCommands...); findErr == nil {
		for _, command := range phpCommands {
			if found[command] {
				phpFound = true
				break
			}
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
		if err := a.Trust(ctx); err != nil {
			result.Warnings = append(result.Warnings, "não foi possível confiar na CA local automaticamente; execute `caddy trust` como Administrador")
		}
		_ = a.appendLog("TLS interno ativado")
	} else {
		_ = a.appendLog("TLS interno desativado")
	}
	return result, nil
}

func (a *App) Trust(ctx context.Context) error {
	if err := a.ensureInstallationManifest(ctx); err != nil {
		return err
	}
	caddyClient := a.edgeCaddy()
	if caddyClient.Runner == nil {
		return fmt.Errorf("Caddy WSL não configurado")
	}
	paths := a.Store.Paths()
	if caddyClient.WSL {
		if err := caddyClient.ExportRootCA(ctx, paths.CARootExport); err != nil {
			// A caller that explicitly injected the pre-M8 Windows client is
			// either running a compatibility test or performing a rollback. The
			// production constructor leaves WindowsCaddy empty, so this cannot
			// silently bring the old edge back into the normal install path.
			if a.WindowsCaddy.Runner != nil {
				return a.WindowsCaddy.Trust(ctx)
			}
			return err
		}
		trustedBefore := false
		if runtime.GOOS == "windows" {
			trustedBefore, _ = platform.CARootTrusted(ctx, paths.CARootExport)
		}
		if err := platform.InstallCARoot(ctx, paths.CARootExport); err != nil {
			return err
		}
		if runtime.GOOS == "windows" {
			if thumbprint, thumbprintErr := platform.CARootThumbprint(paths.CARootExport); thumbprintErr == nil {
				ownership := config.OwnershipCreated
				if trustedBefore {
					ownership = config.OwnershipPreexisting
				}
				if updateErr := a.Store.UpdateManifestResource("windows.ca-trust", func(resource *config.ManifestResource) {
					resource.Ownership = ownership
					resource.Fingerprint = thumbprint
					resource.Target = thumbprint
				}); updateErr != nil {
					return updateErr
				}
			}
		}
		return nil
	}
	// Compatibility for a pre-M8 controller that has not been migrated yet.
	return caddyClient.Trust(ctx)
}

// SetProjectTLS changes the HTTPS preference of one registered project. The
// Caddy WSL edge owns the certificate, while the selector keeps the command
// and advertised URL scoped to the requested project.
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
		// Trust is machine state, not project state. It is intentionally kept out
		// of the TLS toggle critical path; the explicit Trust operation remains
		// available from the security/doctor UI.
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
	ctx = platform.WithWSLOperation(ctx, platform.WSLOperationReload)
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
				_ = a.Store.RollbackCaddy()
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
			_ = a.Store.RollbackCaddy()
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
			_ = a.Store.RollbackCaddy()
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
				_ = a.Store.RollbackCaddy()
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
	// Publishing a new Caddy/WSL configuration is itself a managed mutation.
	// Refresh the post-apply fingerprints after releasing the Store lock so a
	// later uninstall distinguishes DevLAN's own reloads from user edits.
	if manifestErr := a.ensureInstallationManifest(ctx); manifestErr != nil {
		result.Warnings = append(result.Warnings, "não foi possível atualizar a proveniência após aplicar a configuração: "+manifestErr.Error())
		result.Status = statusFor(result)
	}
	if fingerprintErr := a.refreshWSLManagedFingerprints(ctx, "wsl.caddy-config"); fingerprintErr != nil {
		result.Warnings = append(result.Warnings, "não foi possível atualizar a fingerprint do Caddy WSL: "+fingerprintErr.Error())
		result.Status = statusFor(result)
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
	caddyClient := a.edgeCaddy()
	if err := caddyClient.Available(ctx); err == nil {
		if err := caddyClient.EnsureRunning(ctx, paths.Caddy); err != nil {
			return result, fmt.Errorf("iniciar/recarregar Caddy WSL único: %w", err)
		}
		if caddyClient.RequireSystemd && !caddyClient.Status(ctx).Running {
			return result, fmt.Errorf("healthcheck Caddy WSL único: serviço systemd não está ativo")
		}
	} else {
		if mode == OperationalStrict {
			return result, fmt.Errorf("Caddy WSL único indisponível")
		}
		result.Warnings = append(result.Warnings, "Caddy WSL único não disponível; reload ignorado")
	}
	if a.DevProxy != nil && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		activeDev := make(map[string]struct{})
		for _, project := range cfg.Projects {
			resolved, resolveErr := cfg.Resolve(project.Name)
			if resolveErr != nil || resolved.Mode != domain.ModeDev {
				continue
			}
			activeDev[project.Name] = struct{}{}
			if proxyErr := a.DevProxy.Ensure(ctx, project, cfg.DevPort(project), cfg.DevCommand(project), cfg.ProjectIdleTimeout(project)); proxyErr != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("proxy dev %s não iniciado: %v", project.Name, proxyErr))
			}
		}
		if proxyErr := a.DevProxy.Prune(activeDev); proxyErr != nil {
			result.Warnings = append(result.Warnings, "listeners dev obsoletos não removidos: "+proxyErr.Error())
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
	// Caddy must issue the LAN certificate for the same address advertised by
	// the URL table. Resolve the automatic address for the generated edge, but
	// keep the persisted preference as "auto" so it can follow network changes.
	if strings.TrimSpace(cfg.LANAddress) == "" || cfg.LANAddress == "auto" {
		if host, hostErr := platform.LANAddress(); hostErr == nil && host != "" {
			cfg.LANAddress = host
		}
	}
	if err := a.Store.Ensure(); err != nil {
		return ApplyResult{}, err
	}
	accessLogPath := filepath.Join(a.Store.Paths().LogsDir, "access.jsonl")
	wslAccessLogPath, err := platform.ToWSLPath(accessLogPath)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("resolver caminho do access log no WSL: %w", err)
	}
	// The WSL Caddy is the only HTTP edge after M8. It binds 80/443 and the
	// assigned project ports directly; the Windows side receives only the
	// loopback dashboard API.
	unified, err := caddy.RenderWSLUnifiedWithAccessLog(cfg, wslAccessLogPath)
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
	caddyReady := false
	if validate || reload {
		if err := a.edgeCaddy().Available(ctx); err != nil {
			if mode == OperationalStrict {
				return result, fmt.Errorf("Caddy WSL único indisponível: %w", err)
			}
			result.Warnings = append(result.Warnings, "Caddy WSL único não disponível; validação/reload externo ignorado")
		} else {
			caddyReady = true
		}
	}

	validator := func(caddyTemp string) error {
		if validate && caddyReady {
			if err := a.edgeCaddy().Validate(ctx, caddyTemp); err != nil {
				return fmt.Errorf("Caddy WSL único: %w", err)
			}
		}
		return nil
	}
	var callback func(string) error
	if validate {
		callback = validator
	}
	if err := a.Store.ApplyCaddy(unified, callback); err != nil {
		return result, err
	}
	if err := a.Store.ApplyPHPFiles(phpFiles, infoPage); err != nil {
		_ = a.Store.RollbackCaddy()
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
		if caddyReady {
			if err := a.edgeCaddy().EnsureRunning(ctx, paths.Caddy); err != nil {
				_ = a.Store.RollbackCaddy()
				_ = a.Store.RollbackPHPFiles()
				return result, fmt.Errorf("iniciar/recarregar Caddy WSL único: %w", err)
			}
		}
		if a.DevProxy != nil && os.Getenv("DEVLAN_TEST_MOCK") != "1" {
			activeDev := make(map[string]struct{})
			for _, project := range cfg.Projects {
				resolved, resolveErr := cfg.Resolve(project.Name)
				if resolveErr != nil || resolved.Mode != domain.ModeDev {
					continue
				}
				activeDev[project.Name] = struct{}{}
				if proxyErr := a.DevProxy.Ensure(ctx, project, cfg.DevPort(project), cfg.DevCommand(project), cfg.ProjectIdleTimeout(project)); proxyErr != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("proxy dev %s não iniciado: %v", project.Name, proxyErr))
				}
			}
			if proxyErr := a.DevProxy.Prune(activeDev); proxyErr != nil {
				result.Warnings = append(result.Warnings, "listeners dev obsoletos não removidos: "+proxyErr.Error())
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
		discovered, err := a.Detector.BatchDiscoverProjects(platform.WithWSLOperation(ctx, platform.WSLOperationDiscovery), park.Path)
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
	paths := make([]string, 0, len(cfg.Projects))
	for _, project := range cfg.Projects {
		if strings.HasPrefix(project.Path, "/") {
			paths = append(paths, project.Path)
		}
	}
	if len(paths) > 0 {
		if err := a.WSL.GrantProjectsAccess(ctx, paths...); err != nil {
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
	ctx = platform.WithWSLOperation(ctx, platform.WSLOperationDoctor)
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	checks := []Check{}
	if cfg.LANAddress == "auto" {
		if address, err := platform.LANAddress(); err != nil {
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
	if err := caddyClient.Available(ctx); err != nil {
		checks = append(checks, Check{"Caddy WSL único", "WARN", "não encontrado: " + err.Error()})
	} else {
		status := caddyClient.Status(ctx)
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

	if caInfo, err := a.CAInfo(ctx); err == nil && caInfo["exists"] == "true" {
		if runtime.GOOS != "windows" {
			checks = append(checks, Check{"CA Local", "WARN", fmt.Sprintf("certificado raiz presente (%s), mas a confiança só é verificada no Windows", caInfo["path"])})
		} else if trusted, trustErr := platform.CARootTrusted(ctx, caInfo["path"]); trustErr != nil {
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
	generated = extractCaddyLANAddress(a.Store.Paths().Caddy)
	if generated == "" {
		generated = extractCaddyLANAddress(a.Store.Paths().WindowsCaddy)
	}
	if generated != "" && generated != "localhost" && generated != "127.0.0.1" && current != generated {
		return current, generated, true
	}
	return current, generated, false
}

// CaddyTopologyStatus returns the live topology without treating persisted
// files as proof that a process is healthy.
func (a *App) CaddyTopologyStatus(ctx context.Context) platform.TopologySnapshot {
	paths := a.Store.Paths()
	_, unifiedErr := os.Stat(paths.Caddy)
	_, windowsErr := os.Stat(paths.WindowsCaddy)
	_, wslErr := os.Stat(paths.WSLCaddy)
	// Status must not make the legacy Windows admin endpoint part of the normal
	// health graph. Its artifact is enough to identify a partial migration;
	// only the explicit migration coordinator may probe/stop that old process.
	windowsRunning := false
	edge := a.edgeCaddy()
	edgeStatus := edge.Status(ctx)
	return platform.DetectCaddyTopology(unifiedErr == nil, windowsErr == nil, wslErr == nil, windowsRunning, edgeStatus.Running)
}

func restoreManagedFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".devlan-restore-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return platform.AtomicReplaceFile(temporaryName, path)
}

func (a *App) CaddyStatus(ctx context.Context) platform.CaddyServiceStatus {
	return a.edgeCaddy().Status(ctx)
}

func (a *App) WSLCompatibility(ctx context.Context) platform.WSLCompatibilityReport {
	cfg, err := a.Store.Load()
	if err != nil {
		return platform.WSLCompatibilityReport{Checks: []platform.CompatibilityCheck{{Name: "Configuração", Status: platform.CompatibilityFail, Detail: err.Error()}}}
	}
	ports := []int{80, 443}
	base, count := cfg.RouteBasePort, cfg.RoutePortCount
	if base == 0 {
		base = 8080
	}
	if count == 0 {
		count = 100
	}
	for port := base; port < base+count && port <= 65535; port++ {
		ports = append(ports, port)
	}
	effective, effectiveErr := a.EffectiveConfig(ctx, cfg)
	// Only listeners represented by the effective routing table are owned by
	// the live Caddy. An unrelated process using an unassigned port in the
	// configured pool is still a real allocation conflict and must not be
	// hidden merely because Caddy itself is healthy.
	managedPorts := map[int]bool{80: true, 443: true}
	if effectiveErr == nil {
		for _, project := range effective.Projects {
			if resolved, resolveErr := effective.Resolve(project.Name); resolveErr == nil {
				managedPorts[resolved.RoutePort] = true
			}
		}
	}
	for _, port := range cfg.RoutePortAllocations {
		managedPorts[port] = true
	}
	edgeStatus := a.edgeCaddy().Status(ctx)
	edgeLive := edgeStatus.Running && edgeStatus.Live
	lanHost := cfg.LANAddress
	if lanHost == "auto" || strings.TrimSpace(lanHost) == "" {
		lanHost, err = platform.LANAddress()
		if err != nil {
			lanHost = ""
		}
	}
	lanPort := 80
	if effectiveErr == nil && len(effective.Projects) > 0 {
		if resolved, resolveErr := effective.Resolve(effective.Projects[0].Name); resolveErr == nil && resolved.RoutePort > 0 {
			lanPort = resolved.RoutePort
		}
	}
	probe := platform.WSLCompatibilityProbe{
		WSL: a.WSL,
		WSLVersion: func() platform.Runner {
			if a.WSL.Invoker != nil {
				return a.WSL.Invoker
			}
			binary := a.WSL.Binary
			if binary == "" {
				binary = "wsl.exe"
			}
			return platform.NewExecRunner(binary)
		}(),
		ConfigText: func() string {
			path := a.WSLConfigPath
			if path == "" {
				path = platform.UserWSLConfigPath()
			}
			data, _ := os.ReadFile(path)
			return string(data)
		}(),
		PortAvailable: func(_ context.Context, port int) bool {
			if edgeLive && managedPorts[port] {
				return true
			}
			return platform.IsPortAvailable(port)
		},
		LANProbe: func(probeContext context.Context) error {
			if strings.TrimSpace(lanHost) == "" || lanHost == "localhost" || lanHost == "127.0.0.1" || lanHost == "::1" {
				return errors.New("endereço LAN não resolvido")
			}
			// Once the unified edge is prepared, probe the host's physical LAN
			// address from Windows. In mirrored mode this is the same inbound
			// listener a second machine uses; probing that address from inside WSL
			// is not reliable for a host's own interface on every WSL release.
			if _, unifiedErr := os.Stat(a.Store.Paths().Caddy); unifiedErr == nil {
				if probeErr := probeLANTCP(probeContext, lanHost, lanPort); probeErr != nil {
					// Some mirrored WSL builds do not hairpin a Windows
					// connection to the host's own LAN address. Verify the same
					// non-loopback listener from the mirrored Linux interface;
					// host and Hyper-V firewall policy is reconciled separately.
					if wslProbeErr := probeWSLLANTCP(probeContext, a.WSL, lanHost, lanPort); wslProbeErr != nil {
						return fmt.Errorf("probe LAN %s:%d falhou no Windows (%v) e no WSL (%w)", lanHost, lanPort, probeErr, wslProbeErr)
					}
				}
				return nil
			}
			// The probe originates inside the selected WSL distribution and
			// reaches the host's LAN address. It verifies the mirrored path with
			// the same direct listener that a second machine would use, without
			// interpolating the address into shell source.
			const script = `set -e
if command -v curl >/dev/null 2>&1; then
    curl --silent --show-error --connect-timeout 1 --max-time 2 -o /dev/null "http://$1:$2/"
elif command -v wget >/dev/null 2>&1; then
    wget -q -T 2 -O /dev/null "http://$1:$2/"
else
    exit 127
fi`
			_, probeErr := a.WSL.RunOperation(probeContext, platform.WSLOperationDoctor, "/bin/sh", "-c", script, "devlan", lanHost, strconv.Itoa(lanPort))
			if probeErr != nil {
				return fmt.Errorf("probe WSL→LAN %s:%d: %w", lanHost, lanPort, probeErr)
			}
			return nil
		},
		LoopbackProbe: func(probeContext context.Context) error {
			if !a.edgeCaddy().AdminLive(probeContext) {
				return errors.New("probe Windows→WSL no admin do Caddy não respondeu")
			}
			return nil
		},
		WSLToWindowsProbe: func(probeContext context.Context) error {
			return probeWSLToWindowsLoopback(probeContext, a.WSL)
		},
	}
	return probe.Check(ctx, a.WSL.Distribution, ports...)
}

// probeWSLToWindowsLoopback verifies mirrored localhost forwarding without
// depending on the long-running API service. The listener is private,
// ephemeral and exists only for the duration of this probe.
func probeWSLToWindowsLoopback(ctx context.Context, runner platform.WSLRunner) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("abrir listener temporário no Windows: %w", err)
	}
	defer listener.Close()

	serverResult := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverResult <- acceptErr
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
		_, writeErr := connection.Write([]byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n"))
		serverResult <- writeErr
	}()

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	const script = `if command -v curl >/dev/null 2>&1; then
    curl --silent --show-error --connect-timeout 1 --max-time 2 -o /dev/null "http://127.0.0.1:$1/"
elif command -v wget >/dev/null 2>&1; then
    wget -q -T 2 -O /dev/null "http://127.0.0.1:$1/"
else
    exit 127
fi`
	if _, err := runner.RunOperation(probeCtx, platform.WSLOperationDoctor, "/bin/sh", "-c", script, "devlan", port); err != nil {
		return fmt.Errorf("probe WSL→Windows em 127.0.0.1:%s: %w", port, err)
	}
	select {
	case serveErr := <-serverResult:
		if serveErr != nil {
			return fmt.Errorf("responder probe WSL→Windows: %w", serveErr)
		}
		return nil
	case <-probeCtx.Done():
		return probeCtx.Err()
	}
}

func probeWSLLANTCP(ctx context.Context, runner platform.WSLRunner, host string, port int) error {
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	const script = `timeout 2 /bin/bash -c '</dev/tcp/$1/$2' devlan "$1" "$2"`
	if _, err := runner.RunOperation(probeCtx, platform.WSLOperationDoctor, "/bin/bash", "-c", script, "devlan", host, strconv.Itoa(port)); err != nil {
		return err
	}
	return nil
}

// probeLANTCP gives mirrored WSL a short convergence window after the VM is
// restarted. systemd can report Caddy healthy before the host-side mirrored
// listener is reachable; treating that transient as a migration failure leaves
// the operator with an unnecessary rollback.
func probeLANTCP(ctx context.Context, host string, port int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(8 * time.Second)
	var lastErr error
	for {
		attemptContext, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
		lastErr = platform.ProbeTCP(attemptContext, host, port)
		cancel()
		if lastErr == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// MigrateToSingleCaddy applies the M8 topology in a deliberately explicit
// flow. The WSL shutdown is confirmation-gated because it stops every running
// distribution, not only the configured one.
func (a *App) MigrateToSingleCaddy(ctx context.Context, confirmed bool) (platform.MigrationResult, error) {
	if !confirmed {
		return platform.MigrationResult{}, platform.ErrWSLShutdownConfirmation
	}
	a.topologyMu.Lock()
	defer a.topologyMu.Unlock()
	if err := a.ensureInstallationManifest(ctx); err != nil {
		return platform.MigrationResult{}, err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return platform.MigrationResult{}, err
	}
	// The unified edge exposes its listeners directly through mirrored WSL.
	// Reconcile the host and Hyper-V policies before the port handoff so a
	// non-elevated migration fails while the previous Caddy is still serving.
	if os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		if err := a.ensureFirewallSpec(ctx, cfg); err != nil && runtime.GOOS == "windows" {
			return platform.MigrationResult{}, fmt.Errorf("preparar Windows Firewall/Hyper-V Firewall antes da migração: %w; execute o comando em um terminal como Administrador", err)
		}
	}
	paths := a.Store.Paths()
	_, unifiedStatErr := os.Stat(paths.Caddy)
	if unifiedStatErr != nil && !errors.Is(unifiedStatErr, os.ErrNotExist) {
		return platform.MigrationResult{}, fmt.Errorf("ler configuração Caddy WSL: %w", unifiedStatErr)
	}
	unifiedExisted := unifiedStatErr == nil
	preparedUnified := false
	cleanupPrepared := func() {
		if !preparedUnified {
			return
		}
		_ = a.Store.RollbackConfig()
		_ = a.Store.RollbackCaddy()
		_ = a.Store.RollbackPHPFiles()
	}
	if errors.Is(unifiedStatErr, os.ErrNotExist) {
		if _, applyErr := a.saveAndApplyMode(ctx, cfg, false, BootstrapTolerant); applyErr != nil {
			return platform.MigrationResult{}, fmt.Errorf("preparar configuração Caddy WSL: %w", applyErr)
		}
		preparedUnified = true
	}
	legacy := []string{}
	windowsLegacyExisted := false
	wslLegacyExisted := false
	for _, path := range []string{paths.WindowsCaddy, paths.WSLCaddy} {
		if _, statErr := os.Stat(path); statErr == nil {
			legacy = append(legacy, path)
			if path == paths.WindowsCaddy {
				windowsLegacyExisted = true
			} else if path == paths.WSLCaddy {
				wslLegacyExisted = true
			}
		}
	}
	backupRoot := filepath.Join(paths.Dir, "migration-backups", a.now().UTC().Format("20060102-150405.000000000"))
	caddyClient := a.edgeCaddy()
	initialUnifiedRunning := caddyClient.Status(ctx).Running
	unifiedStartAttempted := false
	unifiedStarted := false
	legacyStopAttempted := false
	legacyStopped := false
	legacyCaddy := a.WindowsCaddy
	if legacyCaddy.Runner == nil {
		if _, windowsConfigErr := os.Stat(paths.WindowsCaddy); windowsConfigErr == nil {
			// This adapter is created only inside the migration window. It is not
			// part of the normal M8 lifecycle, but lets an upgrade stop a still
			// running pre-M8 Caddy when its binary is still installed.
			legacyCaddy = platform.NewLocalCaddy("")
		}
	}
	wslConfigPath := a.WSLConfigPath
	if strings.TrimSpace(wslConfigPath) == "" {
		wslConfigPath = platform.UserWSLConfigPath()
	}
	oldWSLConfig, wslConfigErr := os.ReadFile(wslConfigPath)
	if wslConfigErr != nil && !errors.Is(wslConfigErr, os.ErrNotExist) {
		cleanupPrepared()
		return platform.MigrationResult{}, fmt.Errorf("ler .wslconfig: %w", wslConfigErr)
	}
	wslConfigExisted := wslConfigErr == nil
	if wslConfigExisted {
		if err := os.MkdirAll(backupRoot, 0o700); err != nil {
			cleanupPrepared()
			return platform.MigrationResult{}, fmt.Errorf("criar backup da configuração WSL: %w", err)
		}
		if err := os.WriteFile(filepath.Join(backupRoot, "wslconfig"), oldWSLConfig, 0o600); err != nil {
			cleanupPrepared()
			return platform.MigrationResult{}, fmt.Errorf("salvar backup da configuração WSL: %w", err)
		}
	}
	if os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		if _, updateErr := platform.UpdateWSLConfig(wslConfigPath, platform.DefaultWSLConfigSettings()); updateErr != nil {
			cleanupPrepared()
			return platform.MigrationResult{}, updateErr
		}
	}
	restoreWSLConfig := func() error {
		if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
			return nil
		}
		return platform.RestoreWSLConfig(wslConfigPath, oldWSLConfig, wslConfigExisted)
	}

	migration := platform.CaddyMigration{
		UnifiedConfig:   paths.Caddy,
		LegacyFiles:     legacy,
		BackupRoot:      backupRoot,
		ConfirmShutdown: confirmed,
		Now:             a.Now,
		ValidateUnified: func(ctx context.Context) error { return caddyClient.Validate(ctx, paths.Caddy) },
		// Mirrored networking puts the old Windows listeners and the new WSL
		// listeners on the same host namespace. The candidate is validated before
		// this handoff; rollback restarts the legacy edge if the new service does
		// not become healthy.
		StopLegacyBeforeStart: windowsLegacyExisted,
		StartUnified: func(ctx context.Context) error {
			unifiedStartAttempted = true
			status := caddyClient.Status(ctx)
			if caddyClient.RequireSystemd && !status.Systemd {
				// The host .wslconfig change takes effect only after the explicit
				// shutdown. The candidate was already validated; its service is
				// started by the second call after the VM comes back.
				return nil
			}
			if err := caddyClient.EnsureRunning(ctx, paths.Caddy); err != nil {
				return err
			}
			unifiedStarted = true
			return nil
		},
		HealthUnified: func(ctx context.Context) error {
			if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
				return nil
			}
			status := caddyClient.Status(ctx)
			if caddyClient.RequireSystemd && !status.Systemd {
				return nil
			}
			if status.Available && status.Running && status.Live {
				return nil
			}
			return fmt.Errorf("Caddy WSL único não está ativo: %s", status.Detail)
		},
		StopLegacy: func(ctx context.Context) error {
			if !windowsLegacyExisted || legacyCaddy.Runner == nil || !platform.IsAdminResponsive(platform.WindowsCaddyAdminAddress) {
				return nil
			}
			legacyStopAttempted = true
			if err := legacyCaddy.Stop(ctx); err != nil {
				return err
			}
			legacyStopped = true
			return nil
		},
		ShutdownWSL: func(ctx context.Context) error {
			if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
				return nil
			}
			return a.WSL.Shutdown(ctx)
		},
		VerifyAfterWSL: func(ctx context.Context) error {
			if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
				return nil
			}
			report := a.WSLCompatibility(ctx)
			if !report.MirroredNetworking || !report.Systemd || !report.LoopbackBidirectional || !report.LANReachable || len(report.PortConflicts) > 0 {
				return fmt.Errorf("mirrored=%t systemd=%t loopback=%t lan=%t conflicts=%d", report.MirroredNetworking, report.Systemd, report.LoopbackBidirectional, report.LANReachable, len(report.PortConflicts))
			}
			return nil
		},
		RemoveLegacy: func() error {
			for _, path := range legacy {
				if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					return removeErr
				}
			}
			return nil
		},
		Rollback: func(ctx context.Context, backupDir string) error {
			var rollbackErr error
			recordRollbackErr := func(err error) {
				if err != nil && rollbackErr == nil {
					rollbackErr = err
				}
			}
			for _, path := range legacy {
				backupPath := filepath.Join(backupDir, filepath.Base(path))
				data, readErr := os.ReadFile(backupPath)
				if readErr != nil {
					recordRollbackErr(readErr)
					continue
				}
				recordRollbackErr(restoreManagedFile(path, data, 0o644))
			}
			recordRollbackErr(restoreWSLConfig())
			unifiedBackup := filepath.Join(backupDir, "unified.Caddyfile")
			if unifiedExisted {
				data, readErr := os.ReadFile(unifiedBackup)
				if readErr != nil {
					recordRollbackErr(readErr)
				} else {
					restoreErr := restoreManagedFile(paths.Caddy, data, 0o644)
					recordRollbackErr(restoreErr)
					if restoreErr == nil && (initialUnifiedRunning || unifiedStartAttempted) {
						recordRollbackErr(caddyClient.EnsureRunning(ctx, paths.Caddy))
					}
				}
			} else {
				if unifiedStartAttempted || unifiedStarted {
					recordRollbackErr(caddyClient.Stop(ctx))
				}
				recordRollbackErr(os.Remove(paths.Caddy))
				if errors.Is(rollbackErr, os.ErrNotExist) {
					rollbackErr = nil
				}
			}
			if windowsLegacyExisted && legacyCaddy.Runner != nil && (legacyStopped || legacyStopAttempted) {
				recordRollbackErr(legacyCaddy.EnsureRunning(ctx, paths.WindowsCaddy))
			}
			// An explicitly injected legacy WSL adapter is supported for upgrade
			// tests/rollback, but the production App has no second operational
			// client. Never synthesize one here.
			if wslLegacyExisted && a.Caddy.Runner != nil && a.WSLCaddy.Runner != nil {
				recordRollbackErr(a.WSLCaddy.EnsureRunning(ctx, paths.WSLCaddy))
			}
			return rollbackErr
		},
	}
	result, err := migration.Migrate(ctx)
	if err != nil {
		// A validation/start failure can occur before the migration coordinator
		// has a reversible process phase, but it must still not leave the host
		// .wslconfig changed.
		_ = restoreWSLConfig()
		if !unifiedExisted {
			_ = os.Remove(paths.Caddy)
		}
		if preparedUnified {
			cleanupPrepared()
		}
		a.recordTelemetry("topology.migrate", map[string]string{"component": "caddy_wsl", "result": "rolled_back"})
		return result, err
	}
	_ = a.Store.AppendSecurityAudit("CADDY_TOPOLOGY_MIGRATE", "topologia única WSL ativada")
	if recordErr := a.Store.RecordManagedState("windows.wslconfig"); recordErr != nil {
		return result, fmt.Errorf("registrar proveniência de .wslconfig após migração: %w", recordErr)
	}
	if fingerprintErr := a.refreshWSLManagedFingerprints(ctx, "wsl.systemd-config", "wsl.caddy-config"); fingerprintErr != nil {
		return result, fmt.Errorf("registrar proveniência dos arquivos WSL após migração: %w", fingerprintErr)
	}
	a.recordTelemetry("topology.migrate", map[string]string{"component": "caddy_wsl", "result": "ok"})
	return result, nil
}

// RepairM8 reconciles the non-destructive parts of the single-Caddy topology.
// It never calls wsl --shutdown: changing .wslconfig is transactional, but
// applying it to the running VM is intentionally left to the explicit
// migration flow because shutdown terminates every WSL distribution.
func (a *App) RepairM8(ctx context.Context) (ApplyResult, error) {
	result := ApplyResult{}
	a.topologyMu.Lock()
	defer a.topologyMu.Unlock()
	if err := a.ensureInstallationManifest(ctx); err != nil {
		return result, err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return result, err
	}
	if os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		path := a.WSLConfigPath
		if path == "" {
			path = platform.UserWSLConfigPath()
		}
		update, err := platform.UpdateWSLConfig(path, platform.DefaultWSLConfigSettings())
		if err != nil {
			return result, err
		}
		if update.Changed {
			result.Warnings = append(result.Warnings, "o .wslconfig foi atualizado; reinicie o WSL pelo fluxo de migração para aplicar networkingMode=mirrored")
		}
		if recordErr := a.Store.RecordManagedState("windows.wslconfig"); recordErr != nil {
			result.Warnings = append(result.Warnings, "não foi possível registrar a proveniência de .wslconfig: "+recordErr.Error())
		}
		if fingerprintErr := a.refreshWSLManagedFingerprints(ctx, "wsl.systemd-config"); fingerprintErr != nil {
			result.Warnings = append(result.Warnings, "não foi possível registrar a fingerprint de /etc/wsl.conf: "+fingerprintErr.Error())
		}
	}
	if err := a.ensureFirewallSpec(ctx, cfg); err != nil && runtime.GOOS == "windows" {
		return result, fmt.Errorf("reconciliar Windows Firewall/Hyper-V Firewall: %w", err)
	}
	paths := a.Store.Paths()
	caddyStatus := a.CaddyStatus(ctx)
	caddyClient := a.edgeCaddy()
	if caddyClient.RequireSystemd && !caddyStatus.Systemd {
		applied, applyErr := a.saveAndApplyMode(ctx, cfg, false, BootstrapTolerant)
		if applyErr != nil {
			return result, applyErr
		}
		result.Warnings = append(result.Warnings, applied.Warnings...)
		result.Warnings = append(result.Warnings, "systemd do Caddy aguardando o reinício explícito do WSL")
		result.Revision = applied.Revision
	} else if _, err := os.Stat(paths.Caddy); errors.Is(err, os.ErrNotExist) {
		applied, applyErr := a.saveAndApplyMode(ctx, cfg, true, OperationalStrict)
		if applyErr != nil {
			return result, applyErr
		}
		result.Warnings = append(result.Warnings, applied.Warnings...)
		result.Revision = applied.Revision
	} else {
		reloaded, reloadErr := a.Reload(ctx)
		if reloadErr != nil {
			return result, reloadErr
		}
		result.Warnings = append(result.Warnings, reloaded.Warnings...)
		result.Revision = reloaded.Revision
	}
	result.Status = statusFor(result)
	_ = a.Store.AppendSecurityAudit("CADDY_TOPOLOGY_REPAIR", "componentes não destrutivos reconciliados")
	a.recordTelemetry("topology.repair", map[string]string{"component": "caddy_wsl", "result": "ok", "status": result.Status})
	return result, nil
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

func (a *App) CAInfo(ctx context.Context) (map[string]string, error) {
	path := a.Store.Paths().CARootExport
	if _, err := os.Stat(path); err != nil {
		caddyClient := a.edgeCaddy()
		if caddyClient.WSL && caddyClient.Runner != nil {
			if exportErr := caddyClient.ExportRootCA(ctx, path); exportErr != nil {
				path = ""
			}
		} else {
			path = platform.FindCARootCertPath()
		}
	}
	details := platform.ReadCARootDetails(path)
	info := map[string]string{"path": path, "exists": strconv.FormatBool(details.Exists), "valid": strconv.FormatBool(details.Valid)}
	if details.Fingerprint != "" {
		info["fingerprint"] = details.Fingerprint
	}
	trusted := false
	if details.Valid && runtime.GOOS == "windows" {
		if value, trustErr := platform.CARootTrusted(ctx, path); trustErr == nil {
			trusted = value
		}
	}
	info["trusted"] = strconv.FormatBool(trusted)
	if details.NotAfter != "" {
		info["not_after"] = details.NotAfter
	}
	info["renewal_due"] = strconv.FormatBool(details.RenewalDue)
	if details.RemainingDays > 0 {
		info["remaining_days"] = strconv.Itoa(details.RemainingDays)
	}
	if details.Detail != "" {
		info["detail"] = details.Detail
	}
	return info, nil
}

func (a *App) ExportCA(ctx context.Context, targetPath string) (string, error) {
	src := a.Store.Paths().CARootExport
	if _, err := os.Stat(src); err != nil {
		caddyClient := a.edgeCaddy()
		if caddyClient.WSL && caddyClient.Runner != nil {
			if err := caddyClient.ExportRootCA(ctx, src); err != nil {
				return "", err
			}
		} else {
			src = platform.FindCARootCertPath()
		}
	}
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
	if err := platform.ValidateCARootPEM(data); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", fmt.Errorf("criar diretório para certificado %s: %w", targetPath, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(targetPath), ".devlan-ca-export-*.tmp")
	if err != nil {
		return "", err
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("gravar certificado temporário: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := platform.AtomicReplaceFile(temporaryName, targetPath); err != nil {
		return "", fmt.Errorf("publicar certificado em %s: %w", targetPath, err)
	}
	_ = a.Store.AppendSecurityAudit("CA_EXPORT", fmt.Sprintf("target=%s", targetPath))
	return targetPath, nil
}

func (a *App) RotateCA(ctx context.Context) (ApplyResult, error) {
	result := ApplyResult{}
	if err := a.Trust(ctx); err != nil {
		result.Warnings = append(result.Warnings, "não foi possível atualizar a confiança da CA: "+err.Error())
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
	wslStatsData, err := json.MarshalIndent(a.WSL.StatsSnapshot(), "", "  ")
	if err != nil {
		return "", fmt.Errorf("serializar inventário WSL: %w", err)
	}
	topologySnapshot := a.Topology(ctx)
	topologyData, err := json.MarshalIndent(topologySnapshot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("serializar topologia M8: %w", err)
	}
	firewallData, err := json.MarshalIndent(topologySnapshot.Firewall, "", "  ")
	if err != nil {
		return "", fmt.Errorf("serializar estado de firewall: %w", err)
	}

	entries := map[string][]byte{
		"config.json":   exported,
		"doctor.json":   append(doctorData, '\n'),
		"wsl.json":      append(wslStatsData, '\n'),
		"topology.json": append(topologyData, '\n'),
		"firewall.json": append(firewallData, '\n'),
		"runtime.txt":   []byte(fmt.Sprintf("runtime=%s\ndata_dir=%s\n", RuntimeDescription(), a.Store.Paths().Dir)),
	}
	paths := a.Store.Paths()
	for archiveName, sourcePath := range map[string]string{
		"generated/Caddyfile": paths.Caddy,
		"logs/devlan.log":     filepath.Join(paths.LogsDir, "devlan.log"),
		"logs/security.log":   paths.SecurityLog,
	} {
		data, readErr := os.ReadFile(sourcePath)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return "", fmt.Errorf("ler %s para o diagnóstico: %w", sourcePath, readErr)
		}
		if strings.HasSuffix(archiveName, "Caddyfile") {
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
