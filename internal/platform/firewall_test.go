package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

type firewallTestRunner struct {
	showOutput string
	showErr    error
	calls      [][]string
}

func (r *firewallTestRunner) Run(_ context.Context, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(args) >= 5 && args[0] == "advfirewall" && args[2] == "show" {
		return r.showOutput, r.showErr
	}
	return "", nil
}

func managedRuleOutput(spec FirewallSpec) string {
	spec = normalizeFirewallSpec(spec)
	return strings.Join([]string{
		"Rule Name: " + spec.RuleName,
		"Enabled: Yes",
		"Direction: In",
		"Action: Allow",
		"Protocol: TCP",
		"LocalPort: " + spec.localPortExpression(),
		"Profiles: Private",
		"RemoteIP: LocalSubnet",
		"Grouping: " + spec.RuleGroup,
		"Description: " + spec.Description,
	}, "\n")
}

func TestFirewallSpecForConfigCoversPoolButNotUIPort(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.WindowsPort = 80
	cfg.HTTPSPort = 443
	cfg.RouteBasePort = 8080
	cfg.RoutePortCount = 100
	cfg.UIPort = 3210
	port := 9000
	cfg.Projects = []domain.Project{{Name: "outside", Path: "/sites/outside", RoutePort: &port}}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	spec := FirewallSpecForConfig(cfg)
	if len(spec.Ranges) != 1 || spec.Ranges[0] != (PortRange{From: 8080, To: 8179}) {
		t.Fatalf("pool inesperado: %#v", spec.Ranges)
	}
	if !containsInt(spec.Ports, 80) || !containsInt(spec.Ports, 443) || !containsInt(spec.Ports, 9000) || containsInt(spec.Ports, 3210) {
		t.Fatalf("portas inesperadas: %#v", spec.Ports)
	}
}

func TestSystemFirewallReconcilesIdempotentlyAndProtectsThirdPartyRules(t *testing.T) {
	spec := DefaultFirewallSpec()
	runner := &firewallTestRunner{showOutput: managedRuleOutput(spec)}
	adapter := SystemFirewall{Runner: runner}
	if err := adapter.Reconcile(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("regra já correta deveria ser no-op: %#v", runner.calls)
	}

	runner = &firewallTestRunner{showOutput: "Rule Name: DevLAN\nGrouping: Other tool\nDescription: third party"}
	if err := (SystemFirewall{Runner: runner}).Reconcile(context.Background(), spec); !errors.Is(err, ErrFirewallConflict) {
		t.Fatalf("regra de terceiro deveria gerar conflito: %v", err)
	}

	runner = &firewallTestRunner{showOutput: "Rule Name: DevLAN\nGrouping: Other tool\nDescription: third party\n\nRule Name: DevLAN\nGrouping: " + spec.RuleGroup + "\nDescription: " + spec.Description + "\nEnabled: Yes\nDirection: In\nAction: Allow\nProtocol: TCP\nLocalPort: " + spec.localPortExpression() + "\nProfiles: Private\nRemoteIP: LocalSubnet"}
	if err := (SystemFirewall{Runner: runner}).Reconcile(context.Background(), spec); err != nil {
		t.Fatalf("regra gerenciada deveria ser encontrada entre regras com mesmo nome: %v", err)
	}
}

func TestSystemFirewallAddsMissingRuleWithExactProperties(t *testing.T) {
	spec := DefaultFirewallSpec()
	runner := &firewallTestRunner{showErr: ErrFirewallNotFound}
	if err := (SystemFirewall{Runner: runner}).Reconcile(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("esperava consulta e criação, chamadas: %#v", runner.calls)
	}
	joined := strings.Join(runner.calls[1], " ")
	for _, expected := range []string{"dir=in", "action=allow", "protocol=TCP", "profile=private", "remoteip=localsubnet", "localport=80,443,8080-8179", "group=DevLAN Managed"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("propriedade %q ausente: %s", expected, joined)
		}
	}
}

func TestSystemFirewallRejectsInvalidSpecBeforeRunningNetsh(t *testing.T) {
	runner := &firewallTestRunner{showErr: ErrFirewallNotFound}
	if err := (SystemFirewall{Runner: runner}).Reconcile(context.Background(), FirewallSpec{Ports: []int{0}}); err == nil {
		t.Fatal("porta inválida deveria ser rejeitada")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("netsh não deveria ser chamado para uma especificação inválida: %#v", runner.calls)
	}

	runner = &firewallTestRunner{showErr: ErrFirewallNotFound}
	if err := (SystemFirewall{Runner: runner}).Reconcile(context.Background(), FirewallSpec{Ranges: []PortRange{{From: 9000, To: 8999}}}); err == nil {
		t.Fatal("faixa inválida deveria ser rejeitada")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("netsh não deveria ser chamado para uma faixa inválida: %#v", runner.calls)
	}
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
