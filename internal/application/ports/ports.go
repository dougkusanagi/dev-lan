// Package ports contains the narrow interfaces consumed by application
// services. Adapters implement these contracts; transports do not depend on
// concrete persistence or host integrations.
package ports

import (
	"context"
	"sort"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// Store is the smallest persistence boundary needed by the extracted
// application use cases. The concrete config.Store remains free to expose
// recovery and artifact helpers to the composition root without leaking them
// into transports or application services.
type Store interface {
	Load() (domain.Config, error)
	Save(domain.Config) error
}

// ConfigStore is retained as a source-compatible name for the first port
// introduced during R-06a. New code should use Store.
type ConfigStore = Store

// Clock makes expiry and operation timing deterministic in command tests.
type Clock interface {
	Now() time.Time
}

// Runner is the smallest process boundary used by host adapters. It returns
// normalized text and never exposes exec.Cmd or an operating-system process to
// an application use case.
type Runner interface {
	Run(context.Context, ...string) (string, error)
}

// CommandRunner is retained as a source-compatible name for R-06a callers.
type CommandRunner = Runner

// Firewall is the range-aware application port. The adapter translates the
// desired policy to the native Windows/WSL representation and reports health
// without exposing that representation to the use case.
type Firewall interface {
	Reconcile(context.Context, FirewallSpec) error
	Healthy(context.Context, FirewallSpec) (bool, error)
	Remove(context.Context) error
}

// Caddy is the configuration/password surface used by application commands.
// Lifecycle and certificate operations are split into separate ports below so
// a use case only receives the capability it needs.
type Caddy interface {
	Validate(context.Context, string) error
	Reload(context.Context, string) error
	HashPassword(context.Context, string) (string, error)
}

// CaddyLifecycle is the edge lifecycle surface used by the reload use case.
type CaddyLifecycle interface {
	Available(context.Context) error
	EnsureRunning(context.Context, string) error
}

// CaddyCertificates is the public-certificate surface of the Caddy adapter.
// The private CA key never crosses this boundary.
type CaddyCertificates interface {
	ExportRootCA(context.Context, string) error
	Trust(context.Context) error
}

// TrustStore is deliberately separate from CaddyCertificates: Caddy owns CA
// generation while the host owns installation into its user trust store.
type TrustStore interface {
	Install(context.Context, string) error
	IsTrusted(context.Context, string) (bool, error)
}

// NetworkProfile is an adapter-neutral snapshot of the active network policy.
type NetworkProfile struct {
	Public bool
	Detail string
}

// Network contains only observations needed by the application. It does not
// expose net.Interface, listeners or platform-specific command output.
type Network interface {
	LANAddress(context.Context) (string, error)
	ListeningPorts(context.Context) ([]int, error)
	Profile(context.Context) (NetworkProfile, error)
}

type PortRange struct {
	From int
	To   int
}

// FirewallSpec is a pure desired-state value. Keeping it in the port package
// prevents a Windows netsh representation from leaking into application code.
type FirewallSpec struct {
	Ports       []int
	Ranges      []PortRange
	Direction   string
	Action      string
	Protocol    string
	Profile     string
	RemoteIP    string
	RuleName    string
	RuleGroup   string
	Description string
}

const (
	FirewallRuleName        = "DevLAN"
	FirewallRuleGroup       = "DevLAN Managed"
	FirewallRuleDescription = "Managed by DevLAN; do not edit."
)

// DefaultFirewallSpec is the platform-neutral default policy. The adapter is
// responsible for expressing it in the host firewall's native command format.
func DefaultFirewallSpec() FirewallSpec {
	return NormalizeFirewallSpec(FirewallSpec{
		Ports:       []int{80, 443},
		Ranges:      []PortRange{{From: 8080, To: 8179}},
		Direction:   "in",
		Action:      "allow",
		Protocol:    "tcp",
		Profile:     "private",
		RemoteIP:    "localsubnet",
		RuleName:    FirewallRuleName,
		RuleGroup:   FirewallRuleGroup,
		Description: FirewallRuleDescription,
	})
}

// NormalizeFirewallSpec makes comparisons and adapter calls deterministic.
// Invalid ports/ranges are discarded here and validated by the concrete
// adapter before a host mutation.
func NormalizeFirewallSpec(spec FirewallSpec) FirewallSpec {
	if spec.Direction == "" {
		spec.Direction = "in"
	}
	if spec.Action == "" {
		spec.Action = "allow"
	}
	if spec.Protocol == "" {
		spec.Protocol = "tcp"
	}
	if spec.Profile == "" {
		spec.Profile = "private"
	}
	if spec.RemoteIP == "" {
		spec.RemoteIP = "localsubnet"
	}
	if spec.RuleName == "" {
		spec.RuleName = FirewallRuleName
	}
	if spec.RuleGroup == "" {
		spec.RuleGroup = FirewallRuleGroup
	}
	if spec.Description == "" {
		spec.Description = FirewallRuleDescription
	}

	ports := make([]int, 0, len(spec.Ports))
	seen := make(map[int]struct{}, len(spec.Ports))
	for _, port := range spec.Ports {
		if port < 1 || port > 65535 {
			continue
		}
		if _, exists := seen[port]; exists {
			continue
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	sort.Ints(ports)

	ranges := make([]PortRange, 0, len(spec.Ranges))
	for _, portRange := range spec.Ranges {
		if portRange.From < 1 || portRange.To > 65535 || portRange.From > portRange.To {
			continue
		}
		ranges = append(ranges, portRange)
	}
	sort.Slice(ranges, func(left, right int) bool {
		if ranges[left].From == ranges[right].From {
			return ranges[left].To < ranges[right].To
		}
		return ranges[left].From < ranges[right].From
	})
	merged := make([]PortRange, 0, len(ranges))
	for _, current := range ranges {
		if len(merged) == 0 || current.From > merged[len(merged)-1].To+1 {
			merged = append(merged, current)
			continue
		}
		if current.To > merged[len(merged)-1].To {
			merged[len(merged)-1].To = current.To
		}
	}
	filteredPorts := ports[:0]
	for _, port := range ports {
		if !portInRanges(port, merged) {
			filteredPorts = append(filteredPorts, port)
		}
	}
	spec.Ports = filteredPorts
	spec.Ranges = merged
	return spec
}

func portInRanges(port int, ranges []PortRange) bool {
	for _, portRange := range ranges {
		if port >= portRange.From && port <= portRange.To {
			return true
		}
	}
	return false
}

// ReconcilePlan is a persistable description of a mutation. It has no HTTP,
// WSL or filesystem details.
type ReconcilePlan struct {
	OperationID string
	Revision    uint64
	Description string
}

// Reconciler owns the plan/apply/verify lifecycle used by mutating commands.
type Reconciler interface {
	Plan(context.Context, domain.Config) (ReconcilePlan, error)
	Apply(context.Context, ReconcilePlan) error
	Verify(context.Context, ReconcilePlan) error
}
