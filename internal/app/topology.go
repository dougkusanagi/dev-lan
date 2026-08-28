package app

import (
	"context"

	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

// Topology returns a transport-neutral diagnostic view. It does not expose
// persistence, process, or firewall adapters to callers.
func (a *App) Topology(ctx context.Context) TopologySnapshot {
	cfg, cfgErr := a.Store.Load()
	if cfgErr != nil {
		return TopologySnapshot{Error: cfgErr.Error()}
	}
	spec := platform.FirewallSpecForConfig(cfg)
	healthy, firewallErr := a.FirewallHealthy(ctx, cfg)
	topology := a.CaddyTopologyStatus(ctx)
	caddy := a.CaddyStatus(ctx)
	compatibility := a.WSLCompatibility(ctx)
	view := TopologySnapshot{
		Topology:      applicationTopologyStatus(topology),
		Caddy:         applicationCaddyStatus(caddy),
		Compatibility: applicationCompatibilityReport(compatibility),
		Firewall:      FirewallSnapshot{Healthy: healthy, Spec: applicationFirewallSpec(spec)},
	}
	if firewallErr != nil {
		view.Firewall.Detail = firewallErr.Error()
	}
	switch firewall := a.Firewall.(type) {
	case platform.CompositeFirewall:
		status := firewall.HyperVStatus(ctx, spec)
		converted := applicationHyperVStatus(status)
		view.HyperV = &converted
	case *platform.CompositeFirewall:
		if firewall != nil {
			status := firewall.HyperVStatus(ctx, spec)
			converted := applicationHyperVStatus(status)
			view.HyperV = &converted
		}
	}
	if ca, err := a.CAInfo(ctx); err == nil {
		view.CA = ca
	} else {
		view.CA = map[string]string{"error": err.Error()}
	}
	return view
}

func applicationCaddyStatus(status platform.CaddyServiceStatus) application.CaddyStatus {
	return application.CaddyStatus{
		Available: status.Available, Running: status.Running, Systemd: status.Systemd,
		Live: status.Live, AdminAddress: status.AdminAddress, ConfigPath: status.ConfigPath,
		Detail: status.Detail,
	}
}

func applicationTopologyStatus(status platform.TopologySnapshot) application.TopologyStatus {
	return application.TopologyStatus{
		Topology: string(status.Topology), UnifiedConfig: status.UnifiedConfig,
		WindowsConfig: status.WindowsConfig, WSLConfig: status.WSLConfig,
		WindowsRunning: status.WindowsRunning, WSLRunning: status.WSLRunning,
	}
}

func applicationCompatibilityReport(report platform.WSLCompatibilityReport) application.WSLCompatibilityReport {
	converted := application.WSLCompatibilityReport{
		Supported: report.Supported, WindowsVersion: report.WindowsVersion,
		WindowsBuild: report.WindowsBuild, WSLVersion: report.WSLVersion,
		WSL2: report.WSL2, MirroredConfigured: report.MirroredConfigured,
		MirroredNetworking: report.MirroredNetworking, Systemd: report.Systemd,
		LoopbackBidirectional: report.LoopbackBidirectional, LANReachable: report.LANReachable,
		PortConflicts: make([]application.PortConflict, 0, len(report.PortConflicts)),
		Checks:        make([]application.CompatibilityCheck, 0, len(report.Checks)),
	}
	for _, conflict := range report.PortConflicts {
		converted.PortConflicts = append(converted.PortConflicts, application.PortConflict{Port: conflict.Port, Detail: conflict.Detail})
	}
	for _, check := range report.Checks {
		converted.Checks = append(converted.Checks, application.CompatibilityCheck{Name: check.Name, Status: string(check.Status), Detail: check.Detail})
	}
	return converted
}

func applicationFirewallSpec(spec platform.FirewallSpec) application.FirewallSpec {
	converted := application.FirewallSpec{
		Ports: append([]int(nil), spec.Ports...), Direction: spec.Direction,
		Action: spec.Action, Protocol: spec.Protocol, Profile: spec.Profile,
		RemoteIP: spec.RemoteIP, RuleName: spec.RuleName, RuleGroup: spec.RuleGroup,
		Description: spec.Description, Ranges: make([]application.PortRange, 0, len(spec.Ranges)),
	}
	for _, portRange := range spec.Ranges {
		converted.Ranges = append(converted.Ranges, application.PortRange{From: portRange.From, To: portRange.To})
	}
	return converted
}

func applicationHyperVStatus(status platform.HyperVFirewallStatus) application.HyperVFirewallStatus {
	return application.HyperVFirewallStatus{
		Supported: status.Supported, Healthy: status.Healthy, Detail: status.Detail,
		Rule: application.HyperVFirewallRuleState{
			Name: status.Rule.Name, DisplayName: status.Rule.DisplayName, Enabled: status.Rule.Enabled,
			Direction: status.Rule.Direction, Action: status.Rule.Action, Protocol: status.Rule.Protocol,
			LocalPorts: status.Rule.LocalPorts, Profile: status.Rule.Profile,
			RemoteAddress: status.Rule.RemoteAddress, VMCreatorID: status.Rule.VMCreatorID,
		},
		Setting: application.HyperVVMSettingState{
			Name: status.Setting.Name, DefaultInboundAction: status.Setting.DefaultInboundAction,
			LoopbackEnabled: status.Setting.LoopbackEnabled, AllowHostPolicyMerge: status.Setting.AllowHostPolicyMerge,
		},
		Spec: application.HyperVFirewallSpec{
			RuleName: status.Spec.RuleName, DisplayName: status.Spec.DisplayName,
			Ports: append([]int(nil), status.Spec.Ports...), Profile: status.Spec.Profile,
			RemoteAddresses: status.Spec.RemoteAddresses, VMCreatorID: status.Spec.VMCreatorID,
			DefaultInboundAction: status.Spec.DefaultInboundAction, LoopbackEnabled: status.Spec.LoopbackEnabled,
			AllowHostPolicyMerge: status.Spec.AllowHostPolicyMerge,
			Ranges: func() []application.PortRange {
				result := make([]application.PortRange, 0, len(status.Spec.Ranges))
				for _, portRange := range status.Spec.Ranges {
					result = append(result, application.PortRange{From: portRange.From, To: portRange.To})
				}
				return result
			}(),
		},
	}
}
