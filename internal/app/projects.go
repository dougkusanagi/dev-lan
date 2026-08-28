package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/detect"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

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
		if cfg.Projects[i].Name != project.Name {
			continue
		}
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
			framework := detected.JS.Framework
			cfg.Projects[i].DevFramework = &framework
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
	result, err := a.saveAndApply(ctx, cfg, true)
	if err != nil {
		return domain.Project{}, result, err
	}
	_ = a.appendLog(fmt.Sprintf("link %s %s", project.Name, project.Path))
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
	_ = a.appendLog(fmt.Sprintf("unlink %s", project.Name))
	a.recordTelemetry("unlink", map[string]string{"result": "ok"})
	return project, result, nil
}

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
	if err == nil {
		_ = a.appendLog(fmt.Sprintf("projeto estacionado ocultado %s", selected.Name))
	}
	return result, err
}

func (a *App) UnignoreProject(ctx context.Context, projectPath string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	if err := cfg.UnignoreParkProject(projectPath); err != nil {
		return ApplyResult{}, err
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog(fmt.Sprintf("projeto estacionado exibido novamente %s", projectPath))
	}
	return result, err
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
	_ = a.appendLog(fmt.Sprintf("park %s", park.Path))
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
	_ = a.appendLog(fmt.Sprintf("unpark %s", park.Path))
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
		_ = a.appendLog(fmt.Sprintf("modo global %s", mode))
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
			_ = a.appendLog(fmt.Sprintf("modo do projeto %s herdado", name))
		} else {
			_ = a.appendLog(fmt.Sprintf("modo do projeto %s %s", name, *mode))
		}
	}
	return result, err
}
