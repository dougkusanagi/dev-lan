// Package ports contains the narrow interfaces consumed by application
// services. Adapters implement these contracts; transports do not depend on
// concrete persistence or host integrations.
package ports

import (
	"context"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// ConfigStore is the persistence boundary needed by command handlers. The
// concrete config.Store remains free to expose recovery and artifact helpers
// to the composition root without leaking them into transports.
type ConfigStore interface {
	Load() (domain.Config, error)
	Save(domain.Config) error
}

// Clock makes expiry and operation timing deterministic in command tests.
type Clock interface {
	Now() time.Time
}

// CommandRunner is the smallest process boundary used by host adapters.
type CommandRunner interface {
	Run(context.Context, ...string) (string, error)
}

// Firewall is the legacy-compatible minimum port. Implementations may also
// satisfy platform.FirewallReconciler for exact desired-state reconciliation.
type Firewall interface {
	Ensure(context.Context, ...int) error
	Remove(context.Context) error
}

// Caddy is intentionally expressed in terms of the existing stable adapter
// contract while keeping the dependency direction explicit at the use-case
// boundary.
type Caddy interface {
	Validate(context.Context, string) error
	Reload(context.Context, string) error
	HashPassword(context.Context, string) (string, error)
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
