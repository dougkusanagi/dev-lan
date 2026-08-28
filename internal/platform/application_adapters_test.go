package platform

import (
	"context"
	"testing"
	"time"

	applicationports "github.com/dougkusanagi/dev-lan/internal/application/ports"
)

func TestApplicationFirewallUsesTheNeutralPortContract(t *testing.T) {
	native := DefaultFirewallSpec()
	runner := &firewallTestRunner{showOutput: managedRuleOutput(native)}
	adapter := NewApplicationFirewall(SystemFirewall{Runner: runner})
	spec := applicationports.DefaultFirewallSpec()

	healthy, err := adapter.Healthy(context.Background(), spec)
	if err != nil || !healthy {
		t.Fatalf("healthcheck do adapter inesperado: healthy=%t err=%v", healthy, err)
	}
	if err := adapter.Reconcile(context.Background(), spec); err != nil {
		t.Fatalf("reconciliação do adapter inesperada: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("regra já correta deveria ser no-op após duas inspeções, chamadas: %#v", runner.calls)
	}
	for _, call := range runner.calls {
		if len(call) > 2 && (call[2] == "add" || call[2] == "set" || call[2] == "delete") {
			t.Fatalf("adapter alterou regra já correta: %#v", runner.calls)
		}
	}
}

func TestApplicationFirewallPreservesLegacyManagerFallback(t *testing.T) {
	manager := &legacyFirewallAdapter{}
	adapter := NewApplicationFirewall(manager)
	spec := applicationports.FirewallSpec{
		Ports:  []int{443},
		Ranges: []applicationports.PortRange{{From: 9000, To: 9001}},
	}

	if err := adapter.Reconcile(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if len(manager.ensured) != 3 || manager.ensured[0] != 443 || manager.ensured[1] != 9000 || manager.ensured[2] != 9001 {
		t.Fatalf("fallback variádico perdeu a política: %#v", manager.ensured)
	}
	if err := adapter.Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !manager.removed {
		t.Fatal("fallback variádico não removeu a regra")
	}
}

func TestHostNetworkAndSystemClockAcceptCompositionOverrides(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "test")
	called := false
	network := HostNetwork{Listening: func(got context.Context) ([]int, error) {
		called = got.Value(struct{}{}) == "test"
		return []int{3210, 8080}, nil
	}}
	ports, err := network.ListeningPorts(ctx)
	if err != nil || !called || len(ports) != 2 || ports[0] != 3210 || ports[1] != 8080 {
		t.Fatalf("override de listeners inesperado: ports=%v called=%t err=%v", ports, called, err)
	}

	want := time.Date(2026, 8, 28, 18, 0, 0, 0, time.UTC)
	if got := (SystemClock{NowFunc: func() time.Time { return want }}).Now(); !got.Equal(want) {
		t.Fatalf("override de relógio ignorado: got=%s want=%s", got, want)
	}
}

type legacyFirewallAdapter struct {
	ensured []int
	removed bool
}

func (f *legacyFirewallAdapter) Ensure(_ context.Context, ports ...int) error {
	f.ensured = append([]int(nil), ports...)
	return nil
}

func (f *legacyFirewallAdapter) Remove(context.Context) error {
	f.removed = true
	return nil
}
