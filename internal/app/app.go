package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/caddy"
	"github.com/dougkusanagi/dev-lan/internal/config"
	"github.com/dougkusanagi/dev-lan/internal/detect"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	phpconfig "github.com/dougkusanagi/dev-lan/internal/php"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

type App struct {
	Store        config.Store
	Detector     detect.Detector
	WSL          platform.WSLRunner
	PHP          platform.PHPManager
	Dev          platform.DevManager
	WindowsCaddy platform.CaddyClient
	WSLCaddy     platform.CaddyClient
	Now          func() time.Time
}

func New(dataDir string) *App {
	wsl := platform.NewWSLRunner("wsl.exe", "")
	return &App{
		Store:        config.NewStore(dataDir),
		Detector:     detect.Detector{Inspector: detect.SmartInspector{WSL: wsl}},
		WSL:          wsl,
		PHP:          platform.NewWSLPHPManager(wsl),
		Dev:          platform.NewWSLDevManager(wsl),
		WindowsCaddy: platform.NewLocalCaddy(""),
		WSLCaddy:     platform.NewWSLCaddy(wsl),
		Now:          time.Now,
	}
}

type ApplyResult struct {
	Warnings []string
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
	result, err := a.apply(ctx, cfg, false, false)
	if err != nil {
		return result, err
	}
	if err := a.Store.Save(cfg); err != nil {
		_ = a.Store.RollbackGenerated()
		_ = a.Store.RollbackPHPFiles()
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
		ports := []int{cfg.WindowsPort}
		if cfg.TLSEnabled {
			ports = append(ports, cfg.HTTPSPort)
		}
		if err := platform.EnsureFirewall(ctx, ports...); err != nil && runtime.GOOS == "windows" {
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
	_ = a.appendLog("install concluído")
	return result, nil
}

func (a *App) Uninstall(ctx context.Context) (ApplyResult, error) {
	result := ApplyResult{}
	if err := platform.RemoveFirewall(ctx); err != nil && runtime.GOOS == "windows" {
		result.Warnings = append(result.Warnings, "não foi possível remover a regra de firewall DevLAN; execute uninstall como administrador")
	}
	if err := a.Store.RemoveManagedFiles(); err != nil {
		return result, err
	}
	_ = a.appendLog("uninstall concluído")
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
	return project, result, nil
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
	ports := []int{cfg.WindowsPort}
	if enabled {
		ports = append(ports, cfg.HTTPSPort)
	}
	if err := platform.EnsureFirewall(ctx, ports...); err != nil && runtime.GOOS == "windows" {
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
		if err := platform.EnsureFirewall(ctx, cfg.WindowsPort, cfg.HTTPSPort); err != nil && runtime.GOOS == "windows" {
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
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	result, err := a.apply(ctx, cfg, true, true)
	if err == nil {
		_ = a.appendLog("reload aplicado")
	}
	return result, err
}

func (a *App) saveAndApply(ctx context.Context, cfg domain.Config, reload bool) (ApplyResult, error) {
	result, err := a.apply(ctx, cfg, false, reload)
	if err != nil {
		return result, err
	}
	if err := a.Store.Save(cfg); err != nil {
		_ = a.Store.RollbackGenerated()
		return result, err
	}
	return result, nil
}

func (a *App) apply(ctx context.Context, cfg domain.Config, validate, reload bool) (ApplyResult, error) {
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
	wsl, err := caddy.RenderWSL(cfg)
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
			result.Warnings = append(result.Warnings, "Caddy Windows não disponível; validação/reload externo ignorado")
		} else {
			windowsReady = true
		}
		if err := a.WSLCaddy.Available(ctx); err != nil {
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
			if err := a.WSLCaddy.Reload(ctx, paths.WSLCaddy); err != nil {
				_ = a.Store.RollbackGenerated()
				_ = a.Store.RollbackPHPFiles()
				return result, fmt.Errorf("recarregar Caddy WSL: %w", err)
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

	if firewall, err := platform.FirewallRule(ctx, "DevLAN"); err != nil {
		checks = append(checks, Check{"Firewall", "WARN", "regra DevLAN não confirmada"})
	} else if firewall {
		checks = append(checks, Check{"Firewall", "OK", "regra DevLAN encontrada"})
	} else {
		checks = append(checks, Check{"Firewall", "WARN", "regra DevLAN ausente"})
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
	for _, project := range projects {
		resolved, err := effective.Resolve(project.Name)
		if err != nil {
			return nil, err
		}
		switch resolved.Mode {
		case domain.ModePHP:
			detected, detectErr := a.Detector.DetectPHP(ctx, project.Path)
			if detectErr != nil {
				checks = append(checks, Check{"Projeto " + project.Name, "FAIL", detectErr.Error()})
			} else {
				checks = append(checks, Check{"Projeto " + project.Name, "OK", fmt.Sprintf("%s, preset=%s, PHP=%s, pool=%s", detected.DocumentRoot, effective.PHPProjectPreset(project), effective.EffectivePHPVersion(project), phpconfig.PoolSummary(effective, project))})
			}
		case domain.ModeStatic:
			staticRoot := effective.StaticDocumentRoot(project)
			spa := effective.SPAFallback(project)
			checks = append(checks, Check{"Projeto " + project.Name, "OK", fmt.Sprintf("estático: %s (spa_fallback=%t)", staticRoot, spa)})
		case domain.ModeDev:
			devPort := effective.DevPort(project)
			devCmd := effective.DevCommand(project)
			pm := effective.PackageManager(project)
			statusStr := "parado"
			if a.Dev != nil {
				st, _ := a.Dev.Status(ctx, project, devPort)
				statusStr = string(st.State)
			}
			checks = append(checks, Check{"Projeto " + project.Name, "OK", fmt.Sprintf("dev server: %s (porta %d, pm=%s, status=%s)", devCmd, devPort, pm, statusStr)})
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
	stamp := time.Now().Format(time.RFC3339)
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
	port := cfg.DevPort(project)
	cmd := cfg.DevCommand(project)
	if err := a.Dev.StartDev(ctx, project, port, cmd); err != nil {
		return err
	}
	_ = a.appendLog("dev start %s (porta %d)", project.Name, port)
	return nil
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
	if err := a.Dev.StopDev(ctx, project, port); err != nil {
		return err
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
	if err := a.Dev.RestartDev(ctx, project, port, cmd); err != nil {
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
	pm := cfg.PackageManager(project)
	out, err := a.Dev.Build(ctx, project, pm)
	if err == nil {
		_ = a.appendLog("build %s (%s)", project.Name, pm)
	}
	return out, err
}

func (a *App) InstallDeps(ctx context.Context, selector string) (string, error) {
	if a.Dev == nil {
		return "", fmt.Errorf("gerenciador dev não configurado")
	}
	project, cfg, err := a.resolveProject(ctx, selector)
	if err != nil {
		return "", err
	}
	pm := cfg.PackageManager(project)
	out, err := a.Dev.InstallDeps(ctx, project, pm)
	if err == nil {
		_ = a.appendLog("deps install %s (%s)", project.Name, pm)
	}
	return out, err
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
