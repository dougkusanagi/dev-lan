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

	"github.com/dougkusanagi/dev-lan/internal/caddy"
	"github.com/dougkusanagi/dev-lan/internal/config"
	"github.com/dougkusanagi/dev-lan/internal/detect"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	phpconfig "github.com/dougkusanagi/dev-lan/internal/php"
	"github.com/dougkusanagi/dev-lan/internal/platform"
	routealloc "github.com/dougkusanagi/dev-lan/internal/route"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a

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
