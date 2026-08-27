// Package reconcile provides the small, deterministic mutation lifecycle.
// Host-specific work is injected as functions so it can be contract-tested
// without Windows or WSL.
package reconcile

import (
	"context"
	"errors"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/application/ports"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

var ErrInvalidPlan = errors.New("plano de reconciliação inválido")

type Runner struct {
	PlanFunc   func(context.Context, domain.Config) (ports.ReconcilePlan, error)
	ApplyFunc  func(context.Context, ports.ReconcilePlan) error
	VerifyFunc func(context.Context, ports.ReconcilePlan) error
}

func (r Runner) Plan(ctx context.Context, cfg domain.Config) (ports.ReconcilePlan, error) {
	if r.PlanFunc == nil {
		return ports.ReconcilePlan{}, ErrInvalidPlan
	}
	plan, err := r.PlanFunc(ctx, cfg)
	if err != nil {
		return ports.ReconcilePlan{}, err
	}
	if strings.TrimSpace(plan.OperationID) == "" || strings.TrimSpace(plan.Description) == "" {
		return ports.ReconcilePlan{}, ErrInvalidPlan
	}
	return plan, nil
}

func (r Runner) Apply(ctx context.Context, plan ports.ReconcilePlan) error {
	if r.ApplyFunc == nil || strings.TrimSpace(plan.OperationID) == "" {
		return ErrInvalidPlan
	}
	return r.ApplyFunc(ctx, plan)
}

func (r Runner) Verify(ctx context.Context, plan ports.ReconcilePlan) error {
	if r.VerifyFunc == nil || strings.TrimSpace(plan.OperationID) == "" {
		return ErrInvalidPlan
	}
	return r.VerifyFunc(ctx, plan)
}

// Execute makes ordering explicit and refuses to verify a plan that was not
// successfully applied.
func Execute(ctx context.Context, r ports.Reconciler, cfg domain.Config) error {
	plan, err := r.Plan(ctx, cfg)
	if err != nil {
		return err
	}
	if err := r.Apply(ctx, plan); err != nil {
		return err
	}
	return r.Verify(ctx, plan)
}
