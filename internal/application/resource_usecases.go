package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/application/ports"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// ResourceDependencies is the composition boundary for the infrastructure
// use cases extracted in R-06b. Each field is a capability, not a concrete
// operating-system adapter; callers can provide fakes without starting WSL or
// touching a host firewall.
type ResourceDependencies struct {
	Store             ports.Store
	Caddy             ports.Caddy
	CaddyLifecycle    ports.CaddyLifecycle
	CaddyCertificates ports.CaddyCertificates
	Firewall          ports.Firewall
	TrustStore        ports.TrustStore
	Network           ports.Network
	Clock             ports.Clock
}

// ResourceUseCases contains the small cross-cutting commands and queries that
// used to reach directly into platform adapters from internal/app. It is kept
// independent from HTTP, CLI, Wails and persistence records.
type ResourceUseCases struct {
	store             ports.Store
	caddy             ports.Caddy
	caddyLifecycle    ports.CaddyLifecycle
	caddyCertificates ports.CaddyCertificates
	firewall          ports.Firewall
	trustStore        ports.TrustStore
	network           ports.Network
	clock             ports.Clock
}

func NewResourceUseCases(deps ResourceDependencies) *ResourceUseCases {
	return &ResourceUseCases{
		store:             deps.Store,
		caddy:             deps.Caddy,
		caddyLifecycle:    deps.CaddyLifecycle,
		caddyCertificates: deps.CaddyCertificates,
		firewall:          deps.Firewall,
		trustStore:        deps.TrustStore,
		network:           deps.Network,
		clock:             deps.Clock,
	}
}

// ReconcileFirewall loads the authoritative configuration through the Store
// port and sends only a pure desired-state value to the firewall adapter.
func (u *ResourceUseCases) ReconcileFirewall(ctx context.Context) error {
	if u == nil || u.store == nil {
		return ErrUnavailable
	}
	cfg, err := u.store.Load()
	if err != nil {
		return err
	}
	return u.ReconcileFirewallConfig(ctx, cfg)
}

func (u *ResourceUseCases) ReconcileFirewallConfig(ctx context.Context, cfg domain.Config) error {
	if u == nil || u.firewall == nil {
		return ErrUnavailable
	}
	return u.firewall.Reconcile(ctx, FirewallSpecForConfig(cfg))
}

func (u *ResourceUseCases) FirewallHealthy(ctx context.Context, cfg domain.Config) (bool, error) {
	if u == nil || u.firewall == nil {
		return false, ErrUnavailable
	}
	return u.firewall.Healthy(ctx, FirewallSpecForConfig(cfg))
}

func (u *ResourceUseCases) RemoveFirewall(ctx context.Context) error {
	if u == nil || u.firewall == nil {
		return ErrUnavailable
	}
	return u.firewall.Remove(ctx)
}

func (u *ResourceUseCases) HashPassword(ctx context.Context, password string) (string, error) {
	if u == nil || u.caddy == nil {
		return "", ErrUnavailable
	}
	return u.caddy.HashPassword(ctx, password)
}

func (u *ResourceUseCases) ValidateCaddy(ctx context.Context, configPath string) error {
	if u == nil || u.caddy == nil {
		return ErrUnavailable
	}
	return u.caddy.Validate(ctx, configPath)
}

func (u *ResourceUseCases) ReloadCaddy(ctx context.Context, configPath string) error {
	if u == nil || u.caddy == nil {
		return ErrUnavailable
	}
	return u.caddy.Reload(ctx, configPath)
}

// TrustCA keeps Caddy's public-certificate export separate from installation
// into the host trust store. The result is used by the caller only for
// provenance accounting; the private CA key never enters this service.
type TrustResult struct {
	AlreadyTrusted bool
}

func (u *ResourceUseCases) TrustCA(ctx context.Context, certificatePath string) (TrustResult, error) {
	if u == nil || u.caddyCertificates == nil || u.trustStore == nil {
		return TrustResult{}, ErrUnavailable
	}
	if strings.TrimSpace(certificatePath) == "" {
		return TrustResult{}, fmt.Errorf("caminho do certificado raiz vazio")
	}
	if err := u.caddyCertificates.ExportRootCA(ctx, certificatePath); err != nil {
		return TrustResult{}, err
	}
	trustedBefore, _ := u.trustStore.IsTrusted(ctx, certificatePath)
	if err := u.trustStore.Install(ctx, certificatePath); err != nil {
		return TrustResult{}, err
	}
	return TrustResult{AlreadyTrusted: trustedBefore}, nil
}

func (u *ResourceUseCases) IsTrusted(ctx context.Context, certificatePath string) (bool, error) {
	if u == nil || u.trustStore == nil {
		return false, ErrUnavailable
	}
	return u.trustStore.IsTrusted(ctx, certificatePath)
}

func (u *ResourceUseCases) CaddyAvailable(ctx context.Context) error {
	if u == nil || u.caddyLifecycle == nil {
		return ErrUnavailable
	}
	return u.caddyLifecycle.Available(ctx)
}

func (u *ResourceUseCases) EnsureCaddy(ctx context.Context, configPath string) error {
	if u == nil || u.caddyLifecycle == nil {
		return ErrUnavailable
	}
	return u.caddyLifecycle.EnsureRunning(ctx, configPath)
}

// LANAddress resolves the configured address only when the configuration asks
// for automatic selection. It returns an error rather than silently changing a
// caller's policy, so the caller can preserve its existing fallback behavior.
func (u *ResourceUseCases) LANAddress(ctx context.Context, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" || configured != "auto" {
		return configured, nil
	}
	if u == nil || u.network == nil {
		return "", ErrUnavailable
	}
	return u.network.LANAddress(ctx)
}

func (u *ResourceUseCases) NetworkProfile(ctx context.Context) (ports.NetworkProfile, error) {
	if u == nil || u.network == nil {
		return ports.NetworkProfile{}, ErrUnavailable
	}
	return u.network.Profile(ctx)
}

func (u *ResourceUseCases) ExternalListeners(ctx context.Context) ([]int, error) {
	if u == nil || u.network == nil {
		return nil, ErrUnavailable
	}
	return u.network.ListeningPorts(ctx)
}

func (u *ResourceUseCases) Now() time.Time {
	if u != nil && u.clock != nil {
		return u.clock.Now()
	}
	return time.Now()
}

// FirewallSpecForConfig is the application-owned firewall policy. It contains
// no netsh, PowerShell or Windows-specific representation; those decisions
// belong to the adapter implementing ports.Firewall.
func FirewallSpecForConfig(cfg domain.Config) ports.FirewallSpec {
	base, count := cfg.RouteBasePort, cfg.RoutePortCount
	if base == 0 {
		base = 8080
	}
	if count == 0 {
		count = 100
	}

	spec := ports.DefaultFirewallSpec()
	spec.Ports = []int{80, 443}
	spec.Ranges = []ports.PortRange{{From: base, To: base + count - 1}}
	uiPort := cfg.UIPort
	activePaths := make(map[string]struct{}, len(cfg.Projects))
	for _, project := range cfg.Projects {
		activePaths[project.Path] = struct{}{}
		if project.RoutePort != nil && *project.RoutePort > 0 && !firewallPortInRanges(*project.RoutePort, spec.Ranges) && *project.RoutePort != uiPort {
			spec.Ports = append(spec.Ports, *project.RoutePort)
		}
	}
	for path, port := range cfg.RoutePortAllocations {
		if _, active := activePaths[path]; !active || port == uiPort {
			continue
		}
		if !firewallPortInRanges(port, spec.Ranges) {
			spec.Ports = append(spec.Ports, port)
		}
	}
	return ports.NormalizeFirewallSpec(spec)
}

func firewallPortInRanges(port int, ranges []ports.PortRange) bool {
	for _, portRange := range ranges {
		if port >= portRange.From && port <= portRange.To {
			return true
		}
	}
	return false
}
