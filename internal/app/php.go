package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/domain"
	phpconfig "github.com/dougkusanagi/dev-lan/internal/php"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a

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
		_ = a.appendLog(fmt.Sprintf("PHP %s instalado", version))
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
		_ = a.appendLog(fmt.Sprintf("PHP %s removido", version))
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
		_ = a.appendLog(fmt.Sprintf("extensões PHP %s atualizadas", version))
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
		_ = a.appendLog(fmt.Sprintf("PHP global %s", cfg.PHPDefaultVersion))
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
		_ = a.appendLog(fmt.Sprintf("PHP do projeto %s: %s", name, version))
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
		_ = a.appendLog(fmt.Sprintf("preset PHP do projeto %s: %s", name, value))
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
		_ = a.appendLog(fmt.Sprintf("pool PHP do projeto %s: %t", name, isolated))
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
