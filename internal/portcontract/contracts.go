// Package portcontract contains reusable tests for the application ports.
//
// The package is intentionally used only by _test.go files. Each suite accepts
// a factory so the same behavioral contract can run against an in-memory fake,
// an adapter with injected host seams, or a real host integration when a test
// explicitly opts in.
package portcontract

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/application/ports"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

type StoreFactory func(*testing.T) ports.Store

// RunStoreContract verifies that a store can load defaults and round-trip a
// normalized configuration without exposing persistence details to callers.
func RunStoreContract(t *testing.T, newStore StoreFactory) {
	t.Helper()
	store := newStore(t)
	if store == nil {
		t.Fatal("store contract factory returned nil")
	}
	initial, err := store.Load()
	if err != nil {
		t.Fatalf("load inicial: %v", err)
	}
	if err := initial.Validate(); err != nil {
		t.Fatalf("configuração inicial inválida: %v", err)
	}

	want := sampleConfig(t)
	if err := store.Save(want); err != nil {
		t.Fatalf("save da configuração: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("load após save: %v", err)
	}
	// Revision allocation belongs to the store implementation. The logical
	// configuration, including the persisted revision it reports, must survive
	// the round trip.
	want.Revision = got.Revision
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip alterou a configuração:\ngot:  %#v\nwant: %#v", got, want)
	}
}

type RunnerCase struct {
	New        func(*testing.T) ports.Runner
	Args       []string
	WantOutput string
}

// RunRunnerContract verifies the stable text/error-free success surface of a
// runner. Argument shape and platform-specific translation remain assertions
// of the adapter test that supplies the case.
func RunRunnerContract(t *testing.T, tc RunnerCase) {
	t.Helper()
	if tc.New == nil {
		t.Fatal("runner contract factory não configurada")
	}
	runner := tc.New(t)
	if runner == nil {
		t.Fatal("runner contract factory returned nil")
	}
	got, err := runner.Run(context.Background(), tc.Args...)
	if err != nil {
		t.Fatalf("execução do runner: %v", err)
	}
	if got != tc.WantOutput {
		t.Fatalf("saída do runner inesperada: got=%q want=%q", got, tc.WantOutput)
	}
}

type FirewallFactory func(*testing.T) ports.Firewall

// RunFirewallContract covers the desired-state lifecycle shared by fake and
// host firewall adapters: reconcile is idempotent, health reflects the
// requested policy, and removal cannot leave the policy healthy.
func RunFirewallContract(t *testing.T, newFirewall FirewallFactory) {
	t.Helper()
	firewall := newFirewall(t)
	if firewall == nil {
		t.Fatal("firewall contract factory returned nil")
	}
	spec := ports.DefaultFirewallSpec()
	for attempt := 0; attempt < 2; attempt++ {
		if err := firewall.Reconcile(context.Background(), spec); err != nil {
			t.Fatalf("reconciliação %d: %v", attempt+1, err)
		}
	}
	healthy, err := firewall.Healthy(context.Background(), spec)
	if err != nil {
		t.Fatalf("healthcheck após reconciliação: %v", err)
	}
	if !healthy {
		t.Fatal("firewall reconciliado não ficou saudável")
	}
	if err := firewall.Remove(context.Background()); err != nil {
		t.Fatalf("remoção da regra: %v", err)
	}
	healthy, err = firewall.Healthy(context.Background(), spec)
	if err == nil && healthy {
		t.Fatal("firewall removido continua saudável")
	}
}

type CaddyCase struct {
	New        func(*testing.T) ports.Caddy
	ConfigPath string
	Password   string
	WantHash   string
}

// RunCaddyContract checks the common configuration and password operations.
// Lifecycle and certificate responsibilities have separate contracts below.
func RunCaddyContract(t *testing.T, tc CaddyCase) {
	t.Helper()
	if tc.New == nil {
		t.Fatal("Caddy contract factory não configurada")
	}
	if tc.ConfigPath == "" {
		t.Fatal("Caddy contract precisa de um caminho de configuração")
	}
	caddy := tc.New(t)
	if caddy == nil {
		t.Fatal("Caddy contract factory returned nil")
	}
	if err := caddy.Validate(context.Background(), tc.ConfigPath); err != nil {
		t.Fatalf("validação Caddy: %v", err)
	}
	if err := caddy.Reload(context.Background(), tc.ConfigPath); err != nil {
		t.Fatalf("reload Caddy: %v", err)
	}
	hash, err := caddy.HashPassword(context.Background(), tc.Password)
	if err != nil {
		t.Fatalf("hash de senha Caddy: %v", err)
	}
	if hash != tc.WantHash {
		t.Fatalf("hash Caddy inesperado: got=%q want=%q", hash, tc.WantHash)
	}
}

type CaddyLifecycleCase struct {
	New        func(*testing.T) ports.CaddyLifecycle
	ConfigPath string
}

func RunCaddyLifecycleContract(t *testing.T, tc CaddyLifecycleCase) {
	t.Helper()
	if tc.New == nil {
		t.Fatal("lifecycle Caddy contract factory não configurada")
	}
	lifecycle := tc.New(t)
	if lifecycle == nil {
		t.Fatal("lifecycle Caddy contract factory returned nil")
	}
	if err := lifecycle.Available(context.Background()); err != nil {
		t.Fatalf("disponibilidade Caddy: %v", err)
	}
	if err := lifecycle.EnsureRunning(context.Background(), tc.ConfigPath); err != nil {
		t.Fatalf("ensure Caddy: %v", err)
	}
}

type CaddyCertificatesFactory func(*testing.T, []byte) ports.CaddyCertificates

func RunCaddyCertificatesContract(t *testing.T, newCertificates CaddyCertificatesFactory) {
	t.Helper()
	if newCertificates == nil {
		t.Fatal("certificates contract factory não configurada")
	}
	wantCertificate := SampleCARootPEM(t)
	target := filepath.Join(t.TempDir(), "root.crt")
	certificates := newCertificates(t, wantCertificate)
	if certificates == nil {
		t.Fatal("certificates contract factory returned nil")
	}
	if err := certificates.ExportRootCA(context.Background(), target); err != nil {
		t.Fatalf("exportação da CA: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ler CA exportada: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("exportação não produziu certificado PEM")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !certificate.IsCA {
		t.Fatalf("certificado exportado não é uma CA válida: %v", err)
	}
	if err := certificates.Trust(context.Background()); err != nil {
		t.Fatalf("trust da CA: %v", err)
	}
}

type TrustStoreFactory func(*testing.T, string) ports.TrustStore

func RunTrustStoreContract(t *testing.T, newTrustStore TrustStoreFactory) {
	t.Helper()
	if newTrustStore == nil {
		t.Fatal("trust-store contract factory não configurada")
	}
	target := filepath.Join(t.TempDir(), "root.crt")
	if err := os.WriteFile(target, SampleCARootPEM(t), 0o600); err != nil {
		t.Fatalf("preparar CA do trust-store: %v", err)
	}
	trustStore := newTrustStore(t, target)
	if trustStore == nil {
		t.Fatal("trust-store contract factory returned nil")
	}
	if err := trustStore.Install(context.Background(), target); err != nil {
		t.Fatalf("instalação no trust-store: %v", err)
	}
	trusted, err := trustStore.IsTrusted(context.Background(), target)
	if err != nil {
		t.Fatalf("consulta do trust-store: %v", err)
	}
	if !trusted {
		t.Fatal("certificado instalado não foi observado como confiável")
	}
}

type NetworkExpectation struct {
	Address string
	Ports   []int
	Profile ports.NetworkProfile
}

type NetworkFactory func(*testing.T) ports.Network

func RunNetworkContract(t *testing.T, newNetwork NetworkFactory, want NetworkExpectation) {
	t.Helper()
	if newNetwork == nil {
		t.Fatal("network contract factory não configurada")
	}
	network := newNetwork(t)
	if network == nil {
		t.Fatal("network contract factory returned nil")
	}
	address, err := network.LANAddress(context.Background())
	if err != nil {
		t.Fatalf("endereço LAN: %v", err)
	}
	if address != want.Address {
		t.Fatalf("endereço LAN inesperado: got=%q want=%q", address, want.Address)
	}
	portsFound, err := network.ListeningPorts(context.Background())
	if err != nil {
		t.Fatalf("portas em escuta: %v", err)
	}
	if !slices.Equal(portsFound, want.Ports) {
		t.Fatalf("portas em escuta inesperadas: got=%v want=%v", portsFound, want.Ports)
	}
	profile, err := network.Profile(context.Background())
	if err != nil {
		t.Fatalf("perfil de rede: %v", err)
	}
	if profile != want.Profile {
		t.Fatalf("perfil de rede inesperado: got=%#v want=%#v", profile, want.Profile)
	}
}

type ClockFactory func(*testing.T) ports.Clock

func RunClockContract(t *testing.T, newClock ClockFactory, want time.Time) {
	t.Helper()
	if newClock == nil {
		t.Fatal("clock contract factory não configurada")
	}
	clock := newClock(t)
	if clock == nil {
		t.Fatal("clock contract factory returned nil")
	}
	if got := clock.Now(); !got.Equal(want) {
		t.Fatalf("instante do relógio inesperado: got=%s want=%s", got, want)
	}
}

type ReconcilerProbe struct {
	Calls []string
}

type ReconcilerFactory func(*testing.T, *ReconcilerProbe) ports.Reconciler

func RunReconcilerContract(t *testing.T, newReconciler ReconcilerFactory) {
	t.Helper()
	if newReconciler == nil {
		t.Fatal("reconciler contract factory não configurada")
	}
	probe := &ReconcilerProbe{}
	reconciler := newReconciler(t, probe)
	if reconciler == nil {
		t.Fatal("reconciler contract factory returned nil")
	}
	plan, err := reconciler.Plan(context.Background(), domain.NewConfig())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.OperationID == "" || plan.Description == "" {
		t.Fatalf("plan não identificável: %#v", plan)
	}
	if err := reconciler.Apply(context.Background(), plan); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := reconciler.Verify(context.Background(), plan); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !slices.Equal(probe.Calls, []string{"plan", "apply", "verify"}) {
		t.Fatalf("ordem do reconciliador inesperada: got=%v", probe.Calls)
	}
}

// SampleCARootPEM produces a short-lived CA for adapters that validate the
// exported certificate before installing or publishing it.
func SampleCARootPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gerar chave da CA de contrato: %v", err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "DevLAN contract root"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("assinar CA de contrato: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func sampleConfig(t *testing.T) domain.Config {
	t.Helper()
	mode := domain.ModeStatic
	cfg := domain.NewConfig()
	cfg.LANAddress = "192.0.2.44"
	cfg.RouteBasePort = 9000
	cfg.RoutePortCount = 10
	cfg.TLSEnabled = true
	cfg.Projects = []domain.Project{{
		Name: "contract-site",
		Path: "/workspace/contract-site",
		Mode: &mode,
	}}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("configuração de contrato inválida: %v", err)
	}
	return cfg
}
