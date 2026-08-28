package ports_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/application/ports"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/portcontract"
)

func TestFakeAdaptersSatisfyApplicationPortContracts(t *testing.T) {
	t.Run("store", func(t *testing.T) {
		portcontract.RunStoreContract(t, func(*testing.T) ports.Store {
			return &fakeStore{}
		})
	})
	t.Run("runner", func(t *testing.T) {
		portcontract.RunRunnerContract(t, portcontract.RunnerCase{
			New:        func(*testing.T) ports.Runner { return fakeRunner{output: "contract output"} },
			Args:       []string{"contract", "runner"},
			WantOutput: "contract output",
		})
	})
	t.Run("firewall", func(t *testing.T) {
		portcontract.RunFirewallContract(t, func(*testing.T) ports.Firewall {
			return &fakeFirewall{}
		})
	})
	t.Run("caddy", func(t *testing.T) {
		portcontract.RunCaddyContract(t, portcontract.CaddyCase{
			New:        func(*testing.T) ports.Caddy { return fakeCaddy{hash: "$2a$contract"} },
			ConfigPath: "/tmp/devlan-contract.Caddyfile",
			Password:   "contract-password",
			WantHash:   "$2a$contract",
		})
	})
	t.Run("caddy lifecycle", func(t *testing.T) {
		portcontract.RunCaddyLifecycleContract(t, portcontract.CaddyLifecycleCase{
			New:        func(*testing.T) ports.CaddyLifecycle { return fakeCaddyLifecycle{} },
			ConfigPath: "/tmp/devlan-contract.Caddyfile",
		})
	})
	t.Run("caddy certificates", func(t *testing.T) {
		portcontract.RunCaddyCertificatesContract(t, func(_ *testing.T, certificate []byte) ports.CaddyCertificates {
			return fakeCaddyCertificates{certificate: certificate}
		})
	})
	t.Run("trust store", func(t *testing.T) {
		portcontract.RunTrustStoreContract(t, func(_ *testing.T, _ string) ports.TrustStore {
			return &fakeTrustStore{}
		})
	})
	t.Run("network", func(t *testing.T) {
		want := portcontract.NetworkExpectation{
			Address: "192.0.2.44",
			Ports:   []int{3210, 4321},
			Profile: ports.NetworkProfile{Public: false, Detail: "Private (contract)"},
		}
		portcontract.RunNetworkContract(t, func(*testing.T) ports.Network {
			return fakeNetwork{expectation: want}
		}, want)
	})
	t.Run("clock", func(t *testing.T) {
		want := time.Date(2026, 8, 28, 18, 0, 0, 0, time.UTC)
		portcontract.RunClockContract(t, func(*testing.T) ports.Clock {
			return fakeClock{now: want}
		}, want)
	})
	t.Run("reconciler", func(t *testing.T) {
		portcontract.RunReconcilerContract(t, newFakeReconciler)
	})
}

type fakeStore struct {
	cfg   domain.Config
	saved bool
}

func (s *fakeStore) Load() (domain.Config, error) {
	if !s.saved {
		return domain.NewConfig(), nil
	}
	return s.cfg, nil
}

func (s *fakeStore) Save(cfg domain.Config) error {
	if err := cfg.Normalize(); err != nil {
		return err
	}
	if cfg.Revision == 0 {
		cfg.Revision = 1
	}
	s.cfg = cfg
	s.saved = true
	return nil
}

type fakeRunner struct {
	output string
}

func (r fakeRunner) Run(context.Context, ...string) (string, error) { return r.output, nil }

type fakeFirewall struct {
	installed bool
	spec      ports.FirewallSpec
}

func (f *fakeFirewall) Reconcile(_ context.Context, spec ports.FirewallSpec) error {
	f.spec = ports.NormalizeFirewallSpec(spec)
	f.installed = true
	return nil
}

func (f *fakeFirewall) Healthy(_ context.Context, spec ports.FirewallSpec) (bool, error) {
	return f.installed && reflect.DeepEqual(f.spec, ports.NormalizeFirewallSpec(spec)), nil
}

func (f *fakeFirewall) Remove(context.Context) error {
	f.installed = false
	return nil
}

type fakeCaddy struct {
	hash string
}

func (fakeCaddy) Validate(context.Context, string) error { return nil }
func (fakeCaddy) Reload(context.Context, string) error   { return nil }
func (c fakeCaddy) HashPassword(context.Context, string) (string, error) {
	return c.hash, nil
}

type fakeCaddyLifecycle struct{}

func (fakeCaddyLifecycle) Available(context.Context) error { return nil }
func (fakeCaddyLifecycle) EnsureRunning(context.Context, string) error {
	return nil
}

type fakeCaddyCertificates struct {
	certificate []byte
}

func (c fakeCaddyCertificates) ExportRootCA(_ context.Context, target string) error {
	return os.WriteFile(target, c.certificate, 0o600)
}

func (fakeCaddyCertificates) Trust(context.Context) error { return nil }

type fakeTrustStore struct {
	installed string
}

func (s *fakeTrustStore) Install(_ context.Context, certificatePath string) error {
	s.installed = certificatePath
	return nil
}

func (s *fakeTrustStore) IsTrusted(_ context.Context, certificatePath string) (bool, error) {
	return s.installed == certificatePath, nil
}

type fakeNetwork struct {
	expectation portcontract.NetworkExpectation
}

func (n fakeNetwork) LANAddress(context.Context) (string, error) {
	return n.expectation.Address, nil
}

func (n fakeNetwork) ListeningPorts(context.Context) ([]int, error) {
	return append([]int(nil), n.expectation.Ports...), nil
}

func (n fakeNetwork) Profile(context.Context) (ports.NetworkProfile, error) {
	return n.expectation.Profile, nil
}

type fakeClock struct {
	now time.Time
}

func (c fakeClock) Now() time.Time { return c.now }

type fakeReconciler struct {
	probe *portcontract.ReconcilerProbe
	plan  ports.ReconcilePlan
}

func newFakeReconciler(_ *testing.T, probe *portcontract.ReconcilerProbe) ports.Reconciler {
	return fakeReconciler{
		probe: probe,
		plan: ports.ReconcilePlan{
			OperationID: "contract-operation",
			Revision:    1,
			Description: "contract reconciliation",
		},
	}
}

func (r fakeReconciler) Plan(context.Context, domain.Config) (ports.ReconcilePlan, error) {
	r.probe.Calls = append(r.probe.Calls, "plan")
	return r.plan, nil
}

func (r fakeReconciler) Apply(_ context.Context, plan ports.ReconcilePlan) error {
	r.probe.Calls = append(r.probe.Calls, "apply")
	if plan.OperationID != r.plan.OperationID {
		return errors.New("plano inesperado")
	}
	return nil
}

func (r fakeReconciler) Verify(_ context.Context, plan ports.ReconcilePlan) error {
	r.probe.Calls = append(r.probe.Calls, "verify")
	if plan.OperationID != r.plan.OperationID {
		return errors.New("plano inesperado")
	}
	return nil
}
