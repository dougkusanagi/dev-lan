package app

import (
	"context"
	"fmt"
	"sort"

	routealloc "github.com/dougkusanagi/dev-lan/internal/route"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a

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
