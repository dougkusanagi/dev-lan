package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/detect"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

type successfulRunner struct{}

func (successfulRunner) Run(context.Context, ...string) (string, error) { return "", nil }

type fakePHPManager struct {
	installed []string
}

func (f *fakePHPManager) List(context.Context) ([]platform.PHPInstallation, error) {
	result := make([]platform.PHPInstallation, 0, len(f.installed))
	for _, version := range f.installed {
		result = append(result, platform.PHPInstallation{Version: version, FPMBinary: "php-fpm" + version})
	}
	return result, nil
}

func (f *fakePHPManager) Install(_ context.Context, version string, _ []string) error {
	for _, current := range f.installed {
		if current == version {
			return nil
		}
	}
	f.installed = append(f.installed, version)
	return nil
}

func (f *fakePHPManager) Remove(_ context.Context, version string) error {
	for i, current := range f.installed {
		if current == version {
			f.installed = append(f.installed[:i], f.installed[i+1:]...)
			return nil
		}
	}
	return nil
}

func TestParkDiscoversOnlyLaravelChildren(t *testing.T) {
	inspector := detect.StaticInspector{
		Directories: map[string]bool{"/home/dev": true},
		Children: map[string][]string{
			"/home/dev": {"/home/dev/financeiro", "/home/dev/not-a-project"},
		},
		Files: map[string]bool{
			"/home/dev/financeiro/artisan":             true,
			"/home/dev/financeiro/public/index.php":    true,
			"/home/dev/not-a-project/artisan":          false,
			"/home/dev/not-a-project/public/index.php": false,
		},
	}
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: inspector}
	service.WindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.WSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	if _, _, err := service.Park(context.Background(), "/home/dev"); err != nil {
		t.Fatal(err)
	}
	cfg, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	effective, err := service.EffectiveConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(effective.Projects) != 1 || effective.Projects[0].Name != "financeiro" {
		t.Fatalf("descoberta inesperada: %#v", effective.Projects)
	}
	if _, err := effective.Resolve("financeiro"); err != nil {
		t.Fatal(err)
	}
}

func TestLinkRequiresLaravelMarkers(t *testing.T) {
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{}}
	if _, _, err := service.Link(context.Background(), "financeiro", "/home/dev/financeiro"); err == nil {
		t.Fatal("link deveria exigir markers Laravel")
	}
}

func TestSetProjectTLSFindsParkedProject(t *testing.T) {
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{
		Directories: map[string]bool{"/home/dev": true},
		Children:    map[string][]string{"/home/dev": {"/home/dev/financeiro"}},
		Files: map[string]bool{
			"/home/dev/financeiro/artisan":          true,
			"/home/dev/financeiro/public/index.php": true,
		},
	}}
	service.WindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.WSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	if _, _, err := service.Park(context.Background(), "/home/dev"); err != nil {
		t.Fatal(err)
	}
	if _, name, err := service.SetProjectTLS(context.Background(), "financeiro", false); err != nil {
		t.Fatal(err)
	} else if name != "financeiro" {
		t.Fatalf("projeto inesperado: %s", name)
	}
	cfg, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Projects) != 1 || cfg.Projects[0].Secure == nil || *cfg.Projects[0].Secure {
		t.Fatalf("preferência TLS não persistida: %#v", cfg.Projects)
	}
}

func TestUnsecureKeepsTheTLSEdgeAvailableForBrowserCleanup(t *testing.T) {
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{Files: map[string]bool{
		"/home/dev/financeiro/artisan":          true,
		"/home/dev/financeiro/public/index.php": true,
	}}}
	service.WindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.WSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	ctx := context.Background()
	if _, _, err := service.Link(ctx, "financeiro", "/home/dev/financeiro"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.SetProjectTLS(ctx, "financeiro", true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.SetProjectTLS(ctx, "financeiro", false); err != nil {
		t.Fatal(err)
	}
	cfg, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TLSEnabled {
		t.Fatal("listener TLS deveria permanecer disponível para limpar o estado seguro do navegador")
	}
	project, found := cfg.Project("financeiro")
	if !found || project.Secure == nil || *project.Secure {
		t.Fatalf("projeto deveria permanecer anunciado em HTTP: %#v", project)
	}
}

func TestPHPVersionsCanBeInstalledAndSelectedPerProject(t *testing.T) {
	service := New(t.TempDir())
	service.PHP = &fakePHPManager{}
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{Files: map[string]bool{
		"/home/dev/financeiro/artisan":          true,
		"/home/dev/financeiro/public/index.php": true,
	}}}
	service.WindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.WSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	ctx := context.Background()
	if _, err := service.PHPInstall(ctx, "8.3", []string{"xml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PHPInstall(ctx, "8.5", []string{"xml"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Link(ctx, "financeiro", "/home/dev/financeiro"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetProjectPHPVersion(ctx, "financeiro", "8.3"); err != nil {
		t.Fatal(err)
	}
	cfg, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	project, found := cfg.Project("financeiro")
	if !found || project.PHPVersion == nil || *project.PHPVersion != "8.3" {
		t.Fatalf("override PHP não persistido: %#v", cfg.Projects)
	}
	if _, err := service.SetProjectPHPIsolated(ctx, "financeiro", true); err != nil {
		t.Fatal(err)
	}
	_, generated, err := service.Store.Generated()
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) == 0 {
		t.Fatal("Caddyfile não foi gerado")
	}
	data, err := os.ReadFile(service.Store.Paths().PHPGeneratedDir + "/php-8-3.conf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "pm.process_idle_timeout = 10s") || !strings.Contains(string(data), "/run/devlan/php/8.3/financeiro.sock") {
		t.Fatalf("pool PHP incompleto:\n%s", data)
	}
}

func TestAppTrust(t *testing.T) {
	service := New(t.TempDir())
	service.WindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	if err := service.Trust(context.Background()); err != nil {
		t.Fatalf("trust falhou: %v", err)
	}
}

func TestAppDoctorIncludesPortAndIPChecks(t *testing.T) {
	service := New(t.TempDir())
	service.WindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.WSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	checks, err := service.Doctor(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	foundPortHTTP := false
	foundIP := false
	for _, check := range checks {
		if strings.HasPrefix(check.Name, "Porta HTTP") {
			foundPortHTTP = true
		}
		if check.Name == "IP LAN" {
			foundIP = true
		}
	}
	if !foundPortHTTP {
		t.Fatalf("Doctor deveria incluir checagem de porta HTTP: %#v", checks)
	}
	if !foundIP {
		t.Fatalf("Doctor deveria incluir checagem de IP LAN: %#v", checks)
	}
}

func TestAppLANAddressDivergence(t *testing.T) {
	service := New(t.TempDir())
	if err := service.Store.Ensure(); err != nil {
		t.Fatal(err)
	}
	// Write dummy Caddyfile with a fake IP
	fakeCaddyfile := "{\n    default_sni 10.0.0.99\n}\n"
	if err := os.WriteFile(service.Store.Paths().WindowsCaddy, []byte(fakeCaddyfile), 0o644); err != nil {
		t.Fatal(err)
	}
	current, generated, diverged := service.CheckLANAddressDivergence()
	if generated != "10.0.0.99" {
		t.Fatalf("esperado generated IP 10.0.0.99, obtido %q", generated)
	}
	if current != "" && current != "10.0.0.99" && !diverged {
		t.Fatalf("esperado divergência quando IPs forem diferentes (current=%s, generated=%s)", current, generated)
	}
}

func TestLinkJSDevProject(t *testing.T) {
	ctx := context.Background()
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{
		Files: map[string]bool{
			"/home/dev/frontend/package.json":   true,
			"/home/dev/frontend/vite.config.ts": true,
		},
		FileContents: map[string]string{
			"/home/dev/frontend/package.json": `{
				"name": "frontend",
				"packageManager": "pnpm@8.15.0",
				"scripts": {
					"dev": "vite",
					"build": "vite build"
				},
				"devDependencies": {
					"vite": "^5.0.0"
				}
			}`,
		},
	}}
	service.WindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.WSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}

	project, _, err := service.Link(ctx, "frontend", "/home/dev/frontend")
	if err != nil {
		t.Fatalf("falha ao linkar projeto JS Dev: %v", err)
	}
	if project.Mode == nil || *project.Mode != "dev" {
		t.Fatalf("modo dev esperado: %#v", project)
	}
	if project.PackageManager == nil || *project.PackageManager != "pnpm" {
		t.Fatalf("package manager pnpm esperado: %#v", project)
	}
	if project.DevFramework == nil || *project.DevFramework != "vite" {
		t.Fatalf("framework vite esperado: %#v", project)
	}
}

func TestLinkStaticProject(t *testing.T) {
	ctx := context.Background()
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{
		Files: map[string]bool{
			"/home/dev/static-doc/dist/index.html": true,
		},
	}}
	service.WindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.WSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}

	project, _, err := service.Link(ctx, "docs", "/home/dev/static-doc")
	if err != nil {
		t.Fatalf("falha ao linkar projeto estático: %v", err)
	}
	if project.Mode == nil || *project.Mode != "static" {
		t.Fatalf("modo static esperado: %#v", project)
	}
	if project.StaticDir == nil || *project.StaticDir != "dist" {
		t.Fatalf("static dir dist esperado: %#v", project)
	}
}

func TestPhase4AppMethods(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	ctx := context.Background()
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{
		Files: map[string]bool{
			"/home/dev/app/dist/index.html": true,
		},
	}}
	service.WindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.WSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}

	_, _, err := service.Link(ctx, "app", "/home/dev/app")
	if err != nil {
		t.Fatal(err)
	}

	// Test Route Mode
	portMode := domain.RouteModePort
	port := 8088
	if _, err := service.SetRouteMode(ctx, "app", &portMode, &port, nil); err != nil {
		t.Fatalf("SetRouteMode: %v", err)
	}

	// Test Allowlist
	if _, err := service.SetAllowlist(ctx, "app", []string{"192.168.1.100"}); err != nil {
		t.Fatalf("SetAllowlist: %v", err)
	}
	if _, err := service.AddAllowlist(ctx, "app", []string{"10.0.0.1"}); err != nil {
		t.Fatalf("AddAllowlist: %v", err)
	}
	if _, err := service.RemoveAllowlist(ctx, "app", []string{"10.0.0.1"}); err != nil {
		t.Fatalf("RemoveAllowlist: %v", err)
	}

	// Test Expose and Unexpose
	if _, _, err := service.ExposeProject(ctx, "app", 10*time.Minute, nil); err != nil {
		t.Fatalf("ExposeProject: %v", err)
	}
	if _, _, err := service.UnexposeProject(ctx, "app"); err != nil {
		t.Fatalf("UnexposeProject: %v", err)
	}

	// Test Auth
	if _, err := service.SetAuth(ctx, "app", true, "admin", "secret123"); err != nil {
		t.Fatalf("SetAuth: %v", err)
	}
	if _, err := service.DisableAuth(ctx, "app"); err != nil {
		t.Fatalf("DisableAuth: %v", err)
	}

	// Test CA info
	caInfo, err := service.CAInfo(ctx)
	if err != nil {
		t.Fatalf("CAInfo: %v", err)
	}
	if _, found := caInfo["exists"]; !found {
		t.Fatalf("caInfo missing exists key: %#v", caInfo)
	}

	// Test Hosts Entries
	hosts, err := service.HostsEntries(ctx)
	if err != nil {
		t.Fatalf("HostsEntries: %v", err)
	}
	if !strings.Contains(hosts, "DevLAN internal DNS") {
		t.Fatalf("Hosts block missing marker: %s", hosts)
	}

	// Test Security Audit
	logs, err := service.SecurityAuditLogs(ctx, 10)
	if err != nil {
		t.Fatalf("SecurityAuditLogs: %v", err)
	}
	if !strings.Contains(logs, "ROUTE_MODE_CHANGE") {
		t.Fatalf("Audit log missing event: %s", logs)
	}
}

