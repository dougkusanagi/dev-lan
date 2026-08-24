package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/caddy"
	"github.com/dougkusanagi/dev-lan/internal/config"
	"github.com/dougkusanagi/dev-lan/internal/detect"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

type App struct {
	Store        config.Store
	Detector     detect.Detector
	WSL          platform.WSLRunner
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
		return result, err
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

func (a *App) Uninstall(ctx context.Context) error {
	if err := platform.RemoveFirewall(ctx); err != nil && runtime.GOOS == "windows" {
		return fmt.Errorf("remover regra de firewall DevLAN: %w", err)
	}
	if err := a.Store.RemoveManagedFiles(); err != nil {
		return err
	}
	return a.appendLog("arquivos gerenciados removidos; diretórios de projetos preservados")
}

func (a *App) Link(ctx context.Context, name, projectPath string) (domain.Project, ApplyResult, error) {
	normalizedPath, err := domain.NormalizePath(projectPath)
	if err != nil {
		return domain.Project{}, ApplyResult{}, err
	}
	if _, err := a.Detector.Detect(ctx, normalizedPath); err != nil {
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

	if reload {
		paths := a.Store.Paths()
		if windowsReady {
			if err := a.WindowsCaddy.EnsureRunning(ctx, paths.WindowsCaddy); err != nil {
				_ = a.Store.RollbackGenerated()
				return result, fmt.Errorf("recarregar Caddy Windows: %w", err)
			}
		}
		if wslReady {
			if err := a.WSLCaddy.Reload(ctx, paths.WSLCaddy); err != nil {
				_ = a.Store.RollbackGenerated()
				return result, fmt.Errorf("recarregar Caddy WSL: %w", err)
			}
		}
	}
	return result, nil
}

// EffectiveConfig adds Laravel projects discovered as direct children of a
// parked directory. They are intentionally not written to state.json: the
// park is the explicit registration and removing it immediately removes the
// discovered routes.
func (a *App) EffectiveConfig(ctx context.Context, cfg domain.Config) (domain.Config, error) {
	effective := cfg
	knownNames := make(map[string]struct{}, len(cfg.Projects))
	knownPaths := make(map[string]struct{}, len(cfg.Projects))
	for _, project := range cfg.Projects {
		knownNames[project.Name] = struct{}{}
		knownPaths[project.Path] = struct{}{}
	}
	for _, park := range cfg.Parks {
		children, err := a.Detector.Inspector.ListDirectories(ctx, park.Path)
		if err != nil {
			// A parked WSL directory may be temporarily unavailable. Explicit
			// links remain usable, so leave discovery empty and let doctor report
			// the missing runtime.
			if errors.Is(err, platform.ErrUnavailable) {
				continue
			}
			continue
		}
		for _, child := range children {
			childPath, err := domain.NormalizePath(child)
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
			if _, err := a.Detector.Detect(ctx, childPath); err != nil {
				continue
			}
			effective.Projects = append(effective.Projects, domain.Project{Name: name, Path: childPath})
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
			checks = append(checks, Check{"IP LAN", "OK", address})
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
		if resolved.Mode != domain.ModePHP {
			checks = append(checks, Check{"Projeto " + project.Name, "WARN", "modo " + string(resolved.Mode) + " ainda não implementado"})
			continue
		}
		if _, err := a.Detector.Detect(ctx, project.Path); err != nil {
			checks = append(checks, Check{"Projeto " + project.Name, "FAIL", err.Error()})
		} else {
			checks = append(checks, Check{"Projeto " + project.Name, "OK", project.Path + "/public"})
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
	return resolved.URL(host, cfg.WindowsPort, cfg.HTTPSPort, cfg.TLSEnabled), nil
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
		urls = append(urls, item.URL(host, cfg.WindowsPort, cfg.HTTPSPort, cfg.TLSEnabled))
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
