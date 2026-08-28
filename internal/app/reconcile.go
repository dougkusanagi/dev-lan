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

	"github.com/dougkusanagi/dev-lan/internal/application/ports"
	applicationreconcile "github.com/dougkusanagi/dev-lan/internal/application/reconcile"
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
		current, err := a.Store.LoadLocked()
		if err != nil {
			return err
		}
		runner := &configMutationReconciler{
			app:               a,
			current:           current,
			decidePersistence: true,
			reload:            true,
			mode:              OperationalStrict,
			result:            &result,
		}
		if err := applicationreconcile.Execute(ctx, runner, current); err != nil {
			return err
		}
		result.Status = statusFor(result)
		return nil
	})
	if err == nil {
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
		// Keep the Store lock across all three phases. That makes the optimistic
		// revision check in Plan and the SaveLocked commit one atomic mutation
		// from the point of view of CLI, HTTP, Wails and concurrent processes.
		runner := &configMutationReconciler{
			app:     a,
			current: current,
			persist: true,
			reload:  reload,
			mode:    mode,
			result:  &result,
		}
		if err := applicationreconcile.Execute(ctx, runner, cfg); err != nil {
			return err
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

// configMutationReconciler is the composition-root adapter for the generic
// application reconciler. It deliberately keeps persistence and host details
// here, while application/reconcile owns only the ordering contract.
type configMutationReconciler struct {
	app               *App
	current           domain.Config
	desired           domain.Config
	persist           bool
	decidePersistence bool
	reload            bool
	mode              OperationMode
	result            *ApplyResult

	staged           bool
	persistAttempted bool
	applied          bool
	planned          bool
}

var _ ports.Reconciler = (*configMutationReconciler)(nil)

func (r *configMutationReconciler) Plan(ctx context.Context, cfg domain.Config) (ports.ReconcilePlan, error) {
	if err := mutationContextError(ctx); err != nil {
		return ports.ReconcilePlan{}, err
	}
	if cfg.Revision != 0 && cfg.Revision != r.current.Revision {
		return ports.ReconcilePlan{}, fmt.Errorf("%w: esperado %d, atual %d", config.ErrRevisionConflict, cfg.Revision, r.current.Revision)
	}
	desired, err := r.app.routeAllocationConfig(ctx, cfg)
	if err != nil {
		return ports.ReconcilePlan{}, err
	}
	r.desired = desired
	if r.decidePersistence {
		r.persist = !routealloc.EqualAllocations(r.current.RoutePortAllocations, desired.RoutePortAllocations)
	}
	r.planned = true
	revision := r.current.Revision
	if r.persist {
		revision++
	}
	return ports.ReconcilePlan{
		OperationID: NewOperationID(),
		Revision:    revision,
		Description: "aplicar configuração e recursos gerados",
	}, nil
}

func (r *configMutationReconciler) Apply(ctx context.Context, plan ports.ReconcilePlan) error {
	expectedRevision := r.current.Revision
	if r.persist {
		expectedRevision++
	}
	if !r.planned || plan.Revision != expectedRevision {
		return errors.New("plano de reconciliação não corresponde à mutação atual")
	}
	result, err := r.app.apply(ctx, r.desired, true, false, r.mode)
	*r.result = result
	if err != nil {
		r.result.Status = "failed"
		return err
	}
	r.staged = true
	if r.persist {
		r.persistAttempted = true
		if err := r.app.Store.SaveLocked(r.desired); err != nil {
			// SaveLocked may have left a prepared pair or a manifest behind. The
			// same compensating steps used by the former inline pipeline are kept
			// in this phase boundary.
			_ = r.rollbackArtifacts(ctx, false)
			r.result.Status = "failed"
			return err
		}
	}
	r.applied = true
	if r.persist {
		r.result.Revision = plan.Revision
	}
	return nil
}

func (r *configMutationReconciler) Verify(ctx context.Context, plan ports.ReconcilePlan) error {
	expectedRevision := r.current.Revision
	if r.persist {
		expectedRevision++
	}
	if !r.planned || !r.applied || plan.Revision != expectedRevision {
		return errors.New("mutação não foi aplicada antes da verificação")
	}
	if !r.reload {
		return nil
	}
	result, err := r.app.reloadApplied(ctx, r.desired, *r.result, r.mode)
	*r.result = result
	if err == nil {
		return nil
	}

	// A post-commit health failure must restore both the authoritative pair and
	// the generated/runtime artifacts, then attempt to bring the previous
	// revision back online. This is intentionally the old rollback sequence,
	// now owned by the verify phase of the real reconciler.
	_ = r.rollbackArtifacts(ctx, true)
	r.result.Status = "rolled_back"
	return err
}

func (r *configMutationReconciler) rollbackArtifacts(ctx context.Context, restoreRuntime bool) error {
	var rollbackErr error
	if r.persistAttempted {
		if err := r.app.Store.RollbackConfigLocked(); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	if r.staged {
		if err := r.app.Store.RollbackCaddy(); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
		if err := r.app.Store.RollbackPHPFiles(); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	if restoreRuntime {
		if previous, err := r.app.Store.LoadLocked(); err == nil {
			if ctx == nil {
				ctx = context.Background()
			}
			rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
			_, reloadErr := r.app.reloadApplied(rollbackCtx, previous, ApplyResult{}, BootstrapTolerant)
			cancel()
			rollbackErr = errors.Join(rollbackErr, reloadErr)
		} else {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	return rollbackErr
}

func mutationContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
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
