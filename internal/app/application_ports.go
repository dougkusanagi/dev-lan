package app

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/application"
	applicationports "github.com/dougkusanagi/dev-lan/internal/application/ports"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

// The adapters in this file are the only bridge from the legacy platform
// implementations to the small application ports. They deliberately read the
// current App fields on every call, so package-level characterization tests can
// still replace a Caddy, firewall or listener snapshot after construction.

type appStoreAdapter struct{ app *App }

func (p appStoreAdapter) Load() (domain.Config, error) {
	return p.app.Store.Load()
}

func (p appStoreAdapter) Save(cfg domain.Config) error {
	return p.app.Store.Save(cfg)
}

type appClockPort struct{ app *App }

func (p appClockPort) Now() time.Time {
	if p.app != nil && p.app.Now != nil {
		return p.app.Now()
	}
	return time.Now()
}

type appNetworkPort struct{ app *App }

func (p appNetworkPort) LANAddress(context.Context) (string, error) {
	return platform.LANAddress()
}

func (p appNetworkPort) ListeningPorts(ctx context.Context) ([]int, error) {
	if p.app != nil && p.app.ExternalListeners != nil {
		return p.app.ExternalListeners(ctx)
	}
	return platform.ListeningTCPPorts(ctx)
}

func (p appNetworkPort) Profile(ctx context.Context) (applicationports.NetworkProfile, error) {
	public, detail, err := platform.NetworkProfile(ctx)
	return applicationports.NetworkProfile{Public: public, Detail: detail}, err
}

type appCaddyPort struct{ app *App }

func (p appCaddyPort) Validate(ctx context.Context, configPath string) error {
	return p.app.edgeCaddy().Validate(ctx, configPath)
}

func (p appCaddyPort) Reload(ctx context.Context, configPath string) error {
	return p.app.edgeCaddy().Reload(ctx, configPath)
}

func (p appCaddyPort) HashPassword(ctx context.Context, password string) (string, error) {
	return p.app.edgeCaddy().HashPassword(ctx, password)
}

type appCaddyLifecyclePort struct{ app *App }

func (p appCaddyLifecyclePort) Available(ctx context.Context) error {
	return p.app.edgeCaddy().Available(ctx)
}

func (p appCaddyLifecyclePort) EnsureRunning(ctx context.Context, configPath string) error {
	return p.app.edgeCaddy().EnsureRunning(ctx, configPath)
}

type appCaddyCertificatesPort struct{ app *App }

func (p appCaddyCertificatesPort) ExportRootCA(ctx context.Context, targetPath string) error {
	return p.app.edgeCaddy().ExportRootCA(ctx, targetPath)
}

func (p appCaddyCertificatesPort) Trust(ctx context.Context) error {
	return p.app.edgeCaddy().Trust(ctx)
}

type appTrustStorePort struct{}

func (appTrustStorePort) Install(ctx context.Context, certificatePath string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	return platform.InstallCARoot(ctx, certificatePath)
}

func (appTrustStorePort) IsTrusted(ctx context.Context, certificatePath string) (bool, error) {
	return platform.CARootTrusted(ctx, certificatePath)
}

type appFirewallPort struct{ app *App }

func (p appFirewallPort) Reconcile(ctx context.Context, spec applicationports.FirewallSpec) error {
	platformSpec := platformFirewallSpec(spec)
	if p.app == nil || p.app.Firewall == nil {
		return (platform.SystemFirewall{}).Reconcile(ctx, platformSpec)
	}
	if reconciler, ok := p.app.Firewall.(platform.FirewallReconciler); ok {
		return reconciler.Reconcile(ctx, platformSpec)
	}
	manager, ok := p.app.Firewall.(platform.FirewallManager)
	if !ok {
		return fmt.Errorf("adapter de firewall não configurado")
	}
	ports := flattenFirewallPorts(platformSpec)
	return manager.Ensure(ctx, ports...)
}

func (p appFirewallPort) Healthy(ctx context.Context, spec applicationports.FirewallSpec) (bool, error) {
	platformSpec := platformFirewallSpec(spec)
	var rule platform.FirewallRuleState
	var err error
	if p.app == nil || p.app.Firewall == nil {
		rule, err = (platform.SystemFirewall{}).Inspect(ctx)
	} else if reconciler, ok := p.app.Firewall.(platform.FirewallReconciler); ok {
		rule, err = reconciler.Inspect(ctx)
	} else {
		return false, fmt.Errorf("adapter de firewall não oferece inspeção exata")
	}
	if err != nil {
		return false, err
	}
	return rule.Matches(platformSpec), nil
}

func (p appFirewallPort) Remove(ctx context.Context) error {
	if p.app == nil || p.app.Firewall == nil {
		return (platform.SystemFirewall{}).Remove(ctx)
	}
	if reconciler, ok := p.app.Firewall.(platform.FirewallReconciler); ok {
		return reconciler.Remove(ctx)
	}
	if manager, ok := p.app.Firewall.(platform.FirewallManager); ok {
		return manager.Remove(ctx)
	}
	return fmt.Errorf("adapter de firewall não configurado")
}

func platformFirewallSpec(spec applicationports.FirewallSpec) platform.FirewallSpec {
	converted := platform.FirewallSpec{
		Ports:       append([]int(nil), spec.Ports...),
		Direction:   spec.Direction,
		Action:      spec.Action,
		Protocol:    spec.Protocol,
		Profile:     spec.Profile,
		RemoteIP:    spec.RemoteIP,
		RuleName:    spec.RuleName,
		RuleGroup:   spec.RuleGroup,
		Description: spec.Description,
		Ranges:      make([]platform.PortRange, 0, len(spec.Ranges)),
	}
	for _, portRange := range spec.Ranges {
		converted.Ranges = append(converted.Ranges, platform.PortRange{From: portRange.From, To: portRange.To})
	}
	return converted
}

func flattenFirewallPorts(spec platform.FirewallSpec) []int {
	result := append([]int(nil), spec.Ports...)
	for _, portRange := range spec.Ranges {
		for port := portRange.From; port <= portRange.To; port++ {
			result = append(result, port)
		}
	}
	return result
}

func (a *App) resourceUseCases() *application.ResourceUseCases {
	if a == nil {
		return application.NewResourceUseCases(application.ResourceDependencies{})
	}
	return application.NewResourceUseCases(application.ResourceDependencies{
		Store:             appStoreAdapter{app: a},
		Caddy:             appCaddyPort{app: a},
		CaddyLifecycle:    appCaddyLifecyclePort{app: a},
		CaddyCertificates: appCaddyCertificatesPort{app: a},
		Firewall:          appFirewallPort{app: a},
		TrustStore:        appTrustStorePort{},
		Network:           appNetworkPort{app: a},
		Clock:             appClockPort{app: a},
	})
}

var (
	_ applicationports.Store             = appStoreAdapter{}
	_ applicationports.Clock             = appClockPort{}
	_ applicationports.Network           = appNetworkPort{}
	_ applicationports.Caddy             = appCaddyPort{}
	_ applicationports.CaddyLifecycle    = appCaddyLifecyclePort{}
	_ applicationports.CaddyCertificates = appCaddyCertificatesPort{}
	_ applicationports.TrustStore        = appTrustStorePort{}
	_ applicationports.Firewall          = appFirewallPort{}
)
