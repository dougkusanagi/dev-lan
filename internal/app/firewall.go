package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
	routealloc "github.com/dougkusanagi/dev-lan/internal/route"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a

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
