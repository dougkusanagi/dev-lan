package reconcile

import (
	"context"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/application/ports"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/portcontract"
)

func TestRunnerSatisfiesReconcilerPortContract(t *testing.T) {
	portcontract.RunReconcilerContract(t, func(_ *testing.T, probe *portcontract.ReconcilerProbe) ports.Reconciler {
		plan := ports.ReconcilePlan{
			OperationID: "contract-operation",
			Revision:    1,
			Description: "contract reconciliation",
		}
		return Runner{
			PlanFunc: func(context.Context, domain.Config) (ports.ReconcilePlan, error) {
				probe.Calls = append(probe.Calls, "plan")
				return plan, nil
			},
			ApplyFunc: func(context.Context, ports.ReconcilePlan) error {
				probe.Calls = append(probe.Calls, "apply")
				return nil
			},
			VerifyFunc: func(context.Context, ports.ReconcilePlan) error {
				probe.Calls = append(probe.Calls, "verify")
				return nil
			},
		}
	})
}
