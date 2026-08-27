package app

import (
	"context"

	"github.com/dougkusanagi/dev-lan/internal/platform"
)

// FirewallSnapshot is the typed diagnostic representation exposed to support
// bundles and transports. It contains desired state, not an adapter instance.
type FirewallSnapshot struct {
	Healthy bool                  `json:"healthy"`
	Spec    platform.FirewallSpec `json:"spec"`
	Detail  string                `json:"detail,omitempty"`
}

// TopologySnapshot is the stable detailed read model for the single WSL
// Caddy edge. It replaces map[string]any at HTTP and Wails boundaries.
type TopologySnapshot struct {
	Topology      platform.TopologySnapshot       `json:"topology"`
	Caddy         platform.CaddyServiceStatus     `json:"caddy"`
	Compatibility platform.WSLCompatibilityReport `json:"compatibility"`
	Firewall      FirewallSnapshot                `json:"firewall"`
	HyperV        *platform.HyperVFirewallStatus  `json:"hyperv,omitempty"`
	CA            map[string]string               `json:"ca,omitempty"`
	Error         string                          `json:"error,omitempty"`
}

// Topology returns a transport-neutral diagnostic view. It does not expose
// persistence, process, or firewall adapters to callers.
func (a *App) Topology(ctx context.Context) TopologySnapshot {
	cfg, cfgErr := a.Store.Load()
	if cfgErr != nil {
		return TopologySnapshot{Error: cfgErr.Error()}
	}
	spec := platform.FirewallSpecForConfig(cfg)
	healthy, firewallErr := a.FirewallHealthy(ctx, cfg)
	view := TopologySnapshot{
		Topology:      a.CaddyTopologyStatus(ctx),
		Caddy:         a.CaddyStatus(ctx),
		Compatibility: a.WSLCompatibility(ctx),
		Firewall:      FirewallSnapshot{Healthy: healthy, Spec: spec},
	}
	if firewallErr != nil {
		view.Firewall.Detail = firewallErr.Error()
	}
	switch firewall := a.Firewall.(type) {
	case platform.CompositeFirewall:
		status := firewall.HyperVStatus(ctx, spec)
		view.HyperV = &status
	case *platform.CompositeFirewall:
		if firewall != nil {
			status := firewall.HyperVStatus(ctx, spec)
			view.HyperV = &status
		}
	}
	if ca, err := a.CAInfo(ctx); err == nil {
		view.CA = ca
	} else {
		view.CA = map[string]string{"error": err.Error()}
	}
	return view
}
