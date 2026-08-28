package platform

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/application/ports"
	"github.com/dougkusanagi/dev-lan/internal/portcontract"
)

func TestPlatformAdaptersSatisfyApplicationPortContracts(t *testing.T) {
	t.Run("exec runner", func(t *testing.T) {
		portcontract.RunRunnerContract(t, portcontract.RunnerCase{
			New: func(*testing.T) ports.Runner {
				return NewExecRunner(os.Args[0], "-test.run=^TestPortContractExecRunnerHelper$")
			},
			WantOutput: "exec contract output",
		})
	})
	t.Run("WSL runner", func(t *testing.T) {
		portcontract.RunRunnerContract(t, portcontract.RunnerCase{
			New: func(*testing.T) ports.Runner {
				return WSLRunner{
					Binary:  "wsl.exe",
					Invoker: applicationPortRunner{output: "wsl contract output"},
					Stats:   NewWSLStats(),
				}
			},
			Args:       []string{"/bin/true"},
			WantOutput: "wsl contract output",
		})
	})
	t.Run("firewall", func(t *testing.T) {
		portcontract.RunFirewallContract(t, func(*testing.T) ports.Firewall {
			return NewApplicationFirewall(&contractNativeFirewall{})
		})
	})
	t.Run("caddy", func(t *testing.T) {
		portcontract.RunCaddyContract(t, portcontract.CaddyCase{
			New: func(*testing.T) ports.Caddy {
				return CaddyClient{Runner: &applicationPortRunner{output: "$2a$contract"}, Binary: "caddy"}
			},
			ConfigPath: "/tmp/devlan-contract.Caddyfile",
			Password:   "contract-password",
			WantHash:   "$2a$contract",
		})
	})
	t.Run("caddy lifecycle", func(t *testing.T) {
		portcontract.RunCaddyLifecycleContract(t, portcontract.CaddyLifecycleCase{
			New: func(*testing.T) ports.CaddyLifecycle {
				return CaddyClient{Runner: &applicationPortRunner{output: "ok"}, Binary: "caddy"}
			},
			ConfigPath: "/tmp/devlan-contract.Caddyfile",
		})
	})
	t.Run("caddy certificates", func(t *testing.T) {
		portcontract.RunCaddyCertificatesContract(t, func(_ *testing.T, certificate []byte) ports.CaddyCertificates {
			return CaddyClient{
				Runner: &applicationCertificateRunner{certificate: certificate},
				WSL:    true,
			}
		})
	})
	t.Run("trust store", func(t *testing.T) {
		portcontract.RunTrustStoreContract(t, func(t *testing.T, certificatePath string) ports.TrustStore {
			thumbprint, err := CARootThumbprint(certificatePath)
			if err != nil {
				t.Fatalf("thumbprint da CA de contrato: %v", err)
			}
			return WindowsTrustStore{Runner: &trustStoreContractRunner{thumbprint: thumbprint}}
		})
	})
	t.Run("network", func(t *testing.T) {
		want := portcontract.NetworkExpectation{
			Address: "192.0.2.44",
			Ports:   []int{3210, 4321},
			Profile: ports.NetworkProfile{Public: false, Detail: "Private (contract)"},
		}
		portcontract.RunNetworkContract(t, func(*testing.T) ports.Network {
			return HostNetwork{
				LANAddressFunc: func(context.Context) (string, error) { return want.Address, nil },
				Listening:      func(context.Context) ([]int, error) { return append([]int(nil), want.Ports...), nil },
				ProfileFunc:    func(context.Context) (ports.NetworkProfile, error) { return want.Profile, nil },
			}
		}, want)
	})
	t.Run("clock", func(t *testing.T) {
		want := time.Date(2026, 8, 28, 18, 0, 0, 0, time.UTC)
		portcontract.RunClockContract(t, func(*testing.T) ports.Clock {
			return SystemClock{NowFunc: func() time.Time { return want }}
		}, want)
	})
}

func TestPortContractExecRunnerHelper(t *testing.T) {
	for _, argument := range os.Args {
		if argument == "-test.run=^TestPortContractExecRunnerHelper$" {
			fmt.Fprint(os.Stdout, "exec contract output")
			os.Exit(0)
		}
	}
}

type applicationPortRunner struct {
	output string
}

func (r applicationPortRunner) Run(context.Context, ...string) (string, error) {
	return r.output, nil
}

type applicationCertificateRunner struct {
	certificate []byte
}

func (r *applicationCertificateRunner) Run(context.Context, ...string) (string, error) {
	return string(r.certificate), nil
}

func (r *applicationCertificateRunner) RunAsRootOperation(context.Context, string, ...string) (string, error) {
	return string(r.certificate), nil
}

type contractNativeFirewall struct {
	present bool
	state   FirewallRuleState
}

func (f *contractNativeFirewall) Ensure(ctx context.Context, requested ...int) error {
	return f.Reconcile(ctx, FirewallSpec{Ports: append([]int(nil), requested...)})
}

func (f *contractNativeFirewall) Reconcile(_ context.Context, spec FirewallSpec) error {
	spec = normalizeFirewallSpec(spec)
	if err := spec.validate(); err != nil {
		return err
	}
	f.state = FirewallRuleState{
		Name:        spec.RuleName,
		Group:       spec.RuleGroup,
		Description: spec.Description,
		Enabled:     "Yes",
		Direction:   spec.Direction,
		Action:      spec.Action,
		Protocol:    strings.ToUpper(spec.Protocol),
		LocalPorts:  spec.localPortExpression(),
		Profile:     spec.Profile,
		RemoteIP:    spec.RemoteIP,
	}
	f.present = true
	return nil
}

func (f *contractNativeFirewall) Inspect(context.Context) (FirewallRuleState, error) {
	if !f.present {
		return FirewallRuleState{}, ErrFirewallNotFound
	}
	return f.state, nil
}

func (f *contractNativeFirewall) Remove(context.Context) error {
	f.present = false
	return nil
}

type trustStoreContractRunner struct {
	thumbprint string
}

func (r *trustStoreContractRunner) Run(_ context.Context, args ...string) (string, error) {
	for _, argument := range args {
		if argument == "-store" {
			return "Cert Hash(sha1): " + r.thumbprint, nil
		}
	}
	return "", nil
}

var _ ports.Runner = applicationPortRunner{}
var _ ports.Runner = (*applicationCertificateRunner)(nil)
var _ FirewallReconciler = (*contractNativeFirewall)(nil)
var _ ports.Runner = (*trustStoreContractRunner)(nil)
