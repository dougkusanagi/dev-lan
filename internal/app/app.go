package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/config"
	"github.com/dougkusanagi/dev-lan/internal/detect"
	"github.com/dougkusanagi/dev-lan/internal/platform"
	"github.com/dougkusanagi/dev-lan/internal/telemetry"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a
// plaintext credential to be stored as a fallback.
var ErrPasswordHashUnavailable = errors.New("não foi possível gerar o hash da senha; credencial não persistida")

type App struct {
	Store     config.Store
	Detector  detect.Detector
	WSL       platform.WSLRunner
	PHP       platform.PHPManager
	Dev       platform.DevManager
	DevProxy  *platform.DevProxy
	Telemetry telemetry.Store
	// Caddy is the M8 unified edge. It is intentionally optional in the struct
	// so callers compiled against the pre-M8 fields can still inject a WSL
	// client while migrating.
	Caddy        platform.CaddyClient
	WindowsCaddy platform.CaddyClient
	WSLCaddy     platform.CaddyClient
	// Firewall is the small compatibility port. Range-aware implementations may
	// additionally implement platform.FirewallReconciler.
	Firewall platform.FirewallManager
	// ExternalListeners is injectable because a port scan is a host concern,
	// while the allocation policy itself remains pure. Production uses the
	// platform adapter; tests can provide a deterministic snapshot.
	ExternalListeners func(context.Context) ([]int, error)
	Now               func() time.Time
	// WSLConfigPath is injectable for migration tests; empty means the user's
	// host-level .wslconfig.
	WSLConfigPath        string
	mutationMu           sync.Mutex
	topologyMu           sync.Mutex
	operationMu          sync.Mutex
	operations           map[string]OperationState
	operationSubscribers map[*operationSubscriber]struct{}
}

func (a *App) edgeCaddy() platform.CaddyClient {
	// Caddy is the canonical M8 edge. A non-systemd WSLCaddy is accepted only
	// as an explicit compatibility injection from pre-M8 callers/tests; the
	// production constructor leaves it empty and a second systemd edge can
	// never shadow the canonical one.
	if a.Caddy.Runner != nil {
		if a.WSLCaddy.Runner != nil && a.Caddy.RequireSystemd && !a.WSLCaddy.RequireSystemd {
			return a.WSLCaddy
		}
		return a.Caddy
	}
	if a.WSLCaddy.Runner != nil {
		return a.WSLCaddy
	}
	// Compatibility for callers/tests that only injected the old host edge.
	return a.WindowsCaddy
}

type mockRunner struct{}

func (mockRunner) Run(context.Context, ...string) (string, error) {
	// Returning a non-secret sentinel also lets auth characterization tests
	// exercise the successful hash path without starting Caddy.
	return "$2a$10$devlan-test-hash", nil
}

// mockFirewall is selected only by DEVLAN_TEST_MOCK. Keeping this adapter in
// the composition root prevents unit tests from invoking netsh or PowerShell,
// which otherwise causes an interactive Windows Defender Firewall prompt.
type mockFirewall struct{}

func (mockFirewall) Ensure(context.Context, ...int) error { return nil }
func (mockFirewall) Remove(context.Context) error         { return nil }

func New(dataDir string) *App {
	distribution := ""
	if data, err := os.ReadFile(filepath.Join(dataDir, "wsl-distribution")); err == nil {
		distribution = strings.TrimSpace(string(data))
	}
	wsl := platform.NewWSLRunner("wsl.exe", distribution)
	dev := platform.NewWSLDevManager(wsl)
	wslCaddy := platform.NewWSLCaddy(wsl)
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		wslCaddy = platform.CaddyClient{Runner: mockRunner{}, WSL: true}
	}
	var firewall platform.FirewallManager = platform.CompositeFirewall{}
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		firewall = mockFirewall{}
	}
	return &App{
		Store:     config.NewStore(dataDir),
		Detector:  detect.Detector{Inspector: detect.SmartInspector{WSL: wsl}},
		WSL:       wsl,
		PHP:       platform.NewWSLPHPManager(wsl),
		Dev:       dev,
		DevProxy:  platform.NewDevProxy(dev),
		Telemetry: telemetry.NewStore(dataDir),
		// This is the only operational edge. It is deliberately assigned to the
		// canonical field; WSLCaddy below is left nil so a second Caddy cannot be
		// selected accidentally by normal code.
		Caddy: wslCaddy,
		// Windows no longer owns an operational Caddy instance. Keep the field
		// zero-valued so old integrations can still inject a legacy client when
		// explicitly testing or rolling back a migration.
		WindowsCaddy:      platform.CaddyClient{},
		WSLCaddy:          platform.CaddyClient{},
		Firewall:          firewall,
		ExternalListeners: platform.ListeningTCPPorts,
		Now:               time.Now,
		WSLConfigPath:     platform.UserWSLConfigPath(),
	}
}

type ApplyResult struct {
	Warnings []string
	Status   string `json:"status,omitempty"`
	Revision uint64 `json:"revision,omitempty"`
}

type OperationMode string

const (
	BootstrapTolerant OperationMode = "bootstrap-tolerant"
	OperationalStrict OperationMode = "operational-strict"
)
