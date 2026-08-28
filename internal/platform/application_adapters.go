package platform

import (
	"context"
	"fmt"
	"runtime"
	"time"

	applicationports "github.com/dougkusanagi/dev-lan/internal/application/ports"
)

// ApplicationFirewall adapts the native Windows/Hyper-V firewall manager to
// the application-owned, platform-neutral firewall port. Keeping the
// conversion here means application services never need to know about
// netsh, Hyper-V, or the legacy variadic Ensure method.
type ApplicationFirewall struct {
	Manager FirewallManager
}

func NewApplicationFirewall(manager FirewallManager) ApplicationFirewall {
	return ApplicationFirewall{Manager: manager}
}

func (f ApplicationFirewall) Reconcile(ctx context.Context, spec applicationports.FirewallSpec) error {
	native := firewallSpecFromPortSpec(spec)
	if f.Manager == nil {
		return (SystemFirewall{}).Reconcile(ctx, native)
	}
	if reconciler, ok := f.Manager.(FirewallReconciler); ok {
		return reconciler.Reconcile(ctx, native)
	}
	ports := flattenNativeFirewallPorts(native)
	return f.Manager.Ensure(ctx, ports...)
}

func (f ApplicationFirewall) Healthy(ctx context.Context, spec applicationports.FirewallSpec) (bool, error) {
	native := firewallSpecFromPortSpec(spec)
	var (
		state FirewallRuleState
		err   error
	)
	if f.Manager == nil {
		state, err = (SystemFirewall{}).Inspect(ctx)
	} else if reconciler, ok := f.Manager.(FirewallReconciler); ok {
		state, err = reconciler.Inspect(ctx)
	} else {
		return false, fmt.Errorf("adapter de firewall não oferece inspeção exata")
	}
	if err != nil {
		return false, err
	}
	return state.Matches(native), nil
}

func (f ApplicationFirewall) Remove(ctx context.Context) error {
	if f.Manager == nil {
		return (SystemFirewall{}).Remove(ctx)
	}
	if reconciler, ok := f.Manager.(FirewallReconciler); ok {
		return reconciler.Remove(ctx)
	}
	if manager, ok := f.Manager.(FirewallManager); ok {
		return manager.Remove(ctx)
	}
	return fmt.Errorf("adapter de firewall não configurado")
}

func flattenNativeFirewallPorts(spec FirewallSpec) []int {
	result := append([]int(nil), spec.Ports...)
	for _, portRange := range spec.Ranges {
		for port := portRange.From; port <= portRange.To; port++ {
			result = append(result, port)
		}
	}
	return result
}

// WindowsTrustStore is the host trust-store adapter. Caddy owns generation
// and export of the certificate; this adapter owns only installation and
// observation in the current user's Windows Root store.
type WindowsTrustStore struct{}

func (WindowsTrustStore) Install(ctx context.Context, certificatePath string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	return InstallCARoot(ctx, certificatePath)
}

func (WindowsTrustStore) IsTrusted(ctx context.Context, certificatePath string) (bool, error) {
	return CARootTrusted(ctx, certificatePath)
}

// HostNetwork is the Windows-side network adapter consumed by application
// services. The listener callback is injectable for tests and for composition
// roots that already have a snapshot; production falls back to netstat.
type HostNetwork struct {
	Listening func(context.Context) ([]int, error)
}

func (HostNetwork) LANAddress(context.Context) (string, error) {
	return LANAddress()
}

func (n HostNetwork) ListeningPorts(ctx context.Context) ([]int, error) {
	if n.Listening != nil {
		return n.Listening(ctx)
	}
	return ListeningTCPPorts(ctx)
}

func (HostNetwork) Profile(ctx context.Context) (applicationports.NetworkProfile, error) {
	public, detail, err := NetworkProfile(ctx)
	return applicationports.NetworkProfile{Public: public, Detail: detail}, err
}

// SystemClock is the default host clock adapter. The optional function keeps
// expiry and operation tests deterministic without making time a global.
type SystemClock struct {
	NowFunc func() time.Time
}

func (c SystemClock) Now() time.Time {
	if c.NowFunc != nil {
		return c.NowFunc()
	}
	return time.Now()
}

var (
	_ applicationports.Runner            = ExecRunner{}
	_ applicationports.Runner            = WSLRunner{}
	_ applicationports.Caddy             = CaddyClient{}
	_ applicationports.CaddyLifecycle    = CaddyClient{}
	_ applicationports.CaddyCertificates = CaddyClient{}
	_ applicationports.Firewall          = ApplicationFirewall{}
	_ applicationports.TrustStore        = WindowsTrustStore{}
	_ applicationports.Network           = HostNetwork{}
	_ applicationports.Clock             = SystemClock{}
)
