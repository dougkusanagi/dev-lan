package app

import (
	"context"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

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
		_ = a.appendLog(fmt.Sprintf("dev start %s falhou: %v", project.Name, startErr))
		return startErr
	}
	_ = a.appendLog(fmt.Sprintf("dev start %s (porta %d)", project.Name, port))
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
	_ = a.appendLog(fmt.Sprintf("dev stop %s", project.Name))
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
	_ = a.appendLog(fmt.Sprintf("dev restart %s (porta %d)", project.Name, port))
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
	if resolved.Mode == domain.ModeDev || isLaravelDevScript(cfg, project) {
		if err := a.StopDev(ctx, project.Name); err != nil {
			return "", fmt.Errorf("preparar preview LAN: %w", err)
		}
	}
	pm := cfg.PackageManager(project)
	out, err := a.Dev.Build(ctx, project, pm)
	if err == nil {
		_ = a.appendLog(fmt.Sprintf("build %s (%s)", project.Name, pm))
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
		_ = a.appendLog(fmt.Sprintf("deps install %s (%s)", project.Name, pm))
	}
	if a.projectHasManifest(ctx, project, "composer.json") {
		out, installErr := a.RunComposer(ctx, project.Name, "", []string{"--working-dir=" + project.Path, "install", "--no-interaction"})
		outputs = append(outputs, out)
		if installErr != nil {
			return strings.Join(outputs, "\n"), installErr
		}
		_ = a.appendLog(fmt.Sprintf("deps install %s (composer)", project.Name))
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
	if runtime.GOOS != "windows" || !strings.HasPrefix(project.Path, "/") {
		return false
	}
	_, err := a.WSL.Run(ctx, "/usr/bin/test", "-f", pathpkg.Join(project.Path, name))
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

func (a *App) DevStatuses(ctx context.Context, cfg domain.Config, projects []domain.Project) (map[string]platform.DevProcessStatus, error) {
	if a.Dev == nil {
		return nil, fmt.Errorf("gerenciador dev não configurado")
	}
	ctx = platform.WithWSLOperation(ctx, platform.WSLOperationStatus)
	result := make(map[string]platform.DevProcessStatus, len(projects))
	requests := make([]platform.DevStatusRequest, 0, len(projects))
	for _, project := range projects {
		resolved, err := cfg.Resolve(project.Name)
		if err != nil {
			continue
		}
		port := cfg.DevPort(project)
		if a.DevProxy != nil && os.Getenv("DEVLAN_TEST_MOCK") != "1" && a.DevProxy.Has(project.Name) {
			running, starting := a.DevProxy.Status(project.Name)
			state := platform.StateStopped
			if starting {
				state = platform.StateStarting
			} else if running {
				state = platform.StateRunning
			}
			result[project.Name] = platform.DevProcessStatus{ProjectName: project.Name, Port: port, State: state}
			continue
		}
		if resolved.Mode == domain.ModeDev || resolved.Mode == domain.ModeAuto || (resolved.Mode == domain.ModePHP && isLaravelDevScript(cfg, project)) {
			requests = append(requests, platform.DevStatusRequest{Project: project, Port: port})
		}
	}
	if len(requests) == 0 {
		return result, nil
	}
	if batcher, ok := a.Dev.(platform.DevStatusBatcher); ok {
		items, err := batcher.StatusBatch(ctx, requests)
		for _, item := range items {
			result[item.ProjectName] = item
		}
		return result, err
	}
	for _, request := range requests {
		item, err := a.Dev.Status(ctx, request.Project, request.Port)
		if err != nil {
			return result, err
		}
		result[item.ProjectName] = item
	}
	return result, nil
}

func (a *App) DevStatus(ctx context.Context, selector string) (platform.DevProcessStatus, error) {
	if a.Dev == nil {
		return platform.DevProcessStatus{}, fmt.Errorf("gerenciador dev não configurado")
	}
	project, cfg, err := a.resolveProject(ctx, selector)
	if err != nil {
		return platform.DevProcessStatus{}, err
	}
	statuses, statusErr := a.DevStatuses(ctx, cfg, []domain.Project{project})
	if status, ok := statuses[project.Name]; ok {
		return status, statusErr
	}
	if statusErr != nil {
		return platform.DevProcessStatus{}, statusErr
	}
	return a.Dev.Status(ctx, project, cfg.DevPort(project))
}
