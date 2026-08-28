package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/application/ports"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

type resourceStoreFake struct {
	cfg   domain.Config
	loads int
	saves int
}

func (f *resourceStoreFake) Load() (domain.Config, error) {
	f.loads++
	return f.cfg, nil
}

func (f *resourceStoreFake) Save(cfg domain.Config) error {
	f.saves++
	f.cfg = cfg
	return nil
}

type resourceFirewallFake struct {
	spec       ports.FirewallSpec
	reconciles int
	healthy    bool
}

func (f *resourceFirewallFake) Reconcile(_ context.Context, spec ports.FirewallSpec) error {
	f.reconciles++
	f.spec = spec
	return nil
}

func (f *resourceFirewallFake) Healthy(_ context.Context, spec ports.FirewallSpec) (bool, error) {
	f.spec = spec
	return f.healthy, nil
}

func (f *resourceFirewallFake) Remove(context.Context) error { return nil }

type resourceCaddyFake struct {
	trace []string
}

func (f *resourceCaddyFake) Validate(context.Context, string) error { return nil }
func (f *resourceCaddyFake) Reload(context.Context, string) error   { return nil }
func (f *resourceCaddyFake) HashPassword(context.Context, string) (string, error) {
	f.trace = append(f.trace, "hash")
	return "$2a$10$resource-test", nil
}

type resourceCaddyLifecycleFake struct{}

func (resourceCaddyLifecycleFake) Available(context.Context) error { return nil }
func (resourceCaddyLifecycleFake) EnsureRunning(context.Context, string) error {
	return nil
}

type resourceCaddyCertificatesFake struct {
	trace []string
}

func (f *resourceCaddyCertificatesFake) ExportRootCA(context.Context, string) error {
	f.trace = append(f.trace, "export")
	return nil
}

func (f *resourceCaddyCertificatesFake) Trust(context.Context) error {
	f.trace = append(f.trace, "caddy-trust")
	return nil
}

type resourceTrustStoreFake struct {
	trace    []string
	trusted  bool
	installs int
}

func (f *resourceTrustStoreFake) Install(context.Context, string) error {
	f.trace = append(f.trace, "install")
	f.installs++
	return nil
}

func (f *resourceTrustStoreFake) IsTrusted(context.Context, string) (bool, error) {
	f.trace = append(f.trace, "trusted?")
	return f.trusted, nil
}

type resourceNetworkFake struct {
	addressCalls int
	portsCalls   int
}

func (f *resourceNetworkFake) LANAddress(context.Context) (string, error) {
	f.addressCalls++
	return "192.0.2.44", nil
}

func (f *resourceNetworkFake) ListeningPorts(context.Context) ([]int, error) {
	f.portsCalls++
	return []int{4321, 5432}, nil
}

func (f *resourceNetworkFake) Profile(context.Context) (ports.NetworkProfile, error) {
	return ports.NetworkProfile{Public: true, Detail: "Public (fake)"}, nil
}

type resourceClockFake struct{ now time.Time }

func (f resourceClockFake) Now() time.Time { return f.now }

func TestResourceUseCasesBuildsFirewallPolicyThroughPorts(t *testing.T) {
	mode := domain.ModeStatic
	cfg := domain.NewConfig()
	cfg.RouteBasePort = 9000
	cfg.RoutePortCount = 4
	cfg.UIPort = 3210
	cfg.Projects = []domain.Project{
		{Name: "site", Path: "/workspace/site", Mode: &mode, RoutePort: intPointer(9100)},
	}
	cfg.RoutePortAllocations = map[string]int{
		"/workspace/site":   9100,
		"/workspace/active": 9200,
		"/workspace/orphan": 9300,
	}
	store := &resourceStoreFake{cfg: cfg}
	firewall := &resourceFirewallFake{}
	useCases := NewResourceUseCases(ResourceDependencies{Store: store, Firewall: firewall})

	if err := useCases.ReconcileFirewall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.loads != 1 || firewall.reconciles != 1 {
		t.Fatalf("portas não utilizadas como esperado: loads=%d reconciles=%d", store.loads, firewall.reconciles)
	}
	want := ports.FirewallSpec{
		Ports:       []int{80, 443, 9100},
		Ranges:      []ports.PortRange{{From: 9000, To: 9003}},
		Direction:   "in",
		Action:      "allow",
		Protocol:    "tcp",
		Profile:     "private",
		RemoteIP:    "localsubnet",
		RuleName:    ports.FirewallRuleName,
		RuleGroup:   ports.FirewallRuleGroup,
		Description: ports.FirewallRuleDescription,
	}
	if !reflect.DeepEqual(firewall.spec, want) {
		t.Fatalf("política inesperada: got=%#v want=%#v", firewall.spec, want)
	}
	if healthy, err := useCases.FirewallHealthy(context.Background(), cfg); err != nil || healthy {
		t.Fatalf("healthcheck inesperado: healthy=%t err=%v", healthy, err)
	}
}

func TestResourceUseCasesComposeCaddyTrustNetworkAndClockPorts(t *testing.T) {
	caddy := &resourceCaddyFake{}
	certificates := &resourceCaddyCertificatesFake{}
	trustStore := &resourceTrustStoreFake{trusted: true}
	network := &resourceNetworkFake{}
	now := time.Date(2026, 8, 28, 17, 0, 0, 0, time.UTC)
	useCases := NewResourceUseCases(ResourceDependencies{
		Caddy:             caddy,
		CaddyLifecycle:    resourceCaddyLifecycleFake{},
		CaddyCertificates: certificates,
		TrustStore:        trustStore,
		Network:           network,
		Clock:             resourceClockFake{now: now},
	})

	hash, err := useCases.HashPassword(context.Background(), "secret")
	if err != nil || hash != "$2a$10$resource-test" || !reflect.DeepEqual(caddy.trace, []string{"hash"}) {
		t.Fatalf("hashing inesperado: hash=%q trace=%v err=%v", hash, caddy.trace, err)
	}
	trustResult, err := useCases.TrustCA(context.Background(), "C:\\temp\\root.crt")
	if err != nil || !trustResult.AlreadyTrusted {
		t.Fatalf("trust inesperado: result=%#v err=%v", trustResult, err)
	}
	if !reflect.DeepEqual(certificates.trace, []string{"export"}) || !reflect.DeepEqual(trustStore.trace, []string{"trusted?", "install"}) {
		t.Fatalf("ordem de trust inesperada: caddy=%v store=%v", certificates.trace, trustStore.trace)
	}
	if address, err := useCases.LANAddress(context.Background(), "auto"); err != nil || address != "192.0.2.44" {
		t.Fatalf("endereço automático inesperado: address=%q err=%v", address, err)
	}
	if address, err := useCases.LANAddress(context.Background(), ""); err != nil || address != "" {
		t.Fatalf("endereço vazio alterado inesperadamente: address=%q err=%v", address, err)
	}
	if address, err := useCases.LANAddress(context.Background(), "198.51.100.9"); err != nil || address != "198.51.100.9" {
		t.Fatalf("endereço configurado inesperado: address=%q err=%v", address, err)
	}
	profile, err := useCases.NetworkProfile(context.Background())
	if err != nil || !profile.Public || profile.Detail != "Public (fake)" {
		t.Fatalf("perfil de rede inesperado: profile=%#v err=%v", profile, err)
	}
	listeners, err := useCases.ExternalListeners(context.Background())
	if err != nil || !reflect.DeepEqual(listeners, []int{4321, 5432}) || network.portsCalls != 1 {
		t.Fatalf("listeners inesperados: ports=%v calls=%d err=%v", listeners, network.portsCalls, err)
	}
	if got := useCases.Now(); !got.Equal(now) {
		t.Fatalf("relógio injetado ignorado: got=%s want=%s", got, now)
	}
	if network.addressCalls != 1 {
		t.Fatalf("endereço configurado consultou a rede: calls=%d", network.addressCalls)
	}
	if err := useCases.ValidateCaddy(context.Background(), "Caddyfile"); err != nil {
		t.Fatalf("validação Caddy inesperada: %v", err)
	}
	if err := useCases.ReloadCaddy(context.Background(), "Caddyfile"); err != nil {
		t.Fatalf("reload Caddy inesperado: %v", err)
	}
	if err := useCases.CaddyAvailable(context.Background()); err != nil {
		t.Fatalf("disponibilidade Caddy inesperada: %v", err)
	}
	if err := useCases.EnsureCaddy(context.Background(), "Caddyfile"); err != nil {
		t.Fatalf("lifecycle Caddy inesperado: %v", err)
	}
}

func TestResourceUseCasesFailClosedWithoutRequiredPort(t *testing.T) {
	useCases := NewResourceUseCases(ResourceDependencies{})
	if err := useCases.ReconcileFirewall(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("store ausente deveria falhar fechado: %v", err)
	}
	if _, err := useCases.HashPassword(context.Background(), "secret"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Caddy ausente deveria falhar fechado: %v", err)
	}
}

func intPointer(value int) *int { return &value }
