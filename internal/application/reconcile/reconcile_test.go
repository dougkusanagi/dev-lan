package reconcile

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/application/ports"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

func TestExecutePreservesPlanApplyVerifyOrder(t *testing.T) {
	var order []string
	runner := Runner{
		PlanFunc: func(context.Context, domain.Config) (ports.ReconcilePlan, error) {
			order = append(order, "plan")
			return ports.ReconcilePlan{OperationID: "op-1", Description: "test"}, nil
		},
		ApplyFunc: func(context.Context, ports.ReconcilePlan) error {
			order = append(order, "apply")
			return nil
		},
		VerifyFunc: func(context.Context, ports.ReconcilePlan) error {
			order = append(order, "verify")
			return nil
		},
	}
	if err := Execute(context.Background(), runner, domain.NewConfig()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"plan", "apply", "verify"}) {
		t.Fatalf("ordem inesperada: %#v", order)
	}
}

func TestExecuteStopsBeforeVerifyOnApplyFailure(t *testing.T) {
	verifyCalled := false
	want := errors.New("apply failed")
	runner := Runner{
		PlanFunc: func(context.Context, domain.Config) (ports.ReconcilePlan, error) {
			return ports.ReconcilePlan{OperationID: "op-1", Description: "test"}, nil
		},
		ApplyFunc:  func(context.Context, ports.ReconcilePlan) error { return want },
		VerifyFunc: func(context.Context, ports.ReconcilePlan) error { verifyCalled = true; return nil },
	}
	if err := Execute(context.Background(), runner, domain.NewConfig()); !errors.Is(err, want) || verifyCalled {
		t.Fatalf("falha não interrompeu o fluxo: err=%v verify=%t", err, verifyCalled)
	}
}
