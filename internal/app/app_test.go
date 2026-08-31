package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/detect"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

type successfulRunner struct{}

func (successfulRunner) Run(context.Context, ...string) (string, error) { return "", nil }

type unavailableInspector struct{}

func (unavailableInspector) Exists(context.Context, string, string) (bool, error) {
	return false, platform.ErrUnavailable
}
func (unavailableInspector) Directory(context.Context, string) (bool, error) {
	return false, platform.ErrUnavailable
}
func (unavailableInspector) ListDirectories(context.Context, string) ([]string, error) {
	return nil, platform.ErrUnavailable
}
func (unavailableInspector) ReadFile(context.Context, string, string) ([]byte, error) {
	return nil, platform.ErrUnavailable
}

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
	service.legacyWindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.legacyWSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	service.Caddy = platform.CaddyClient{Runner: hashingRunner{}, WSL: true}
	service.Caddy = platform.CaddyClient{Runner: hashingRunner{}, WSL: true}
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

func TestParkKeepsLastKnownAllocatedProjectsWhenDiscoveryIsUnavailable(t *testing.T) {
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: unavailableInspector{}}
	cfg := domain.NewConfig()
	cfg.Parks = []domain.Park{{Path: "/home/dev"}}
	cfg.RoutePortAllocations = map[string]int{"/home/dev/inventory": 8089}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	effective, err := service.EffectiveConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(effective.Projects) != 1 || effective.Projects[0].Name != "inventory" || effective.Projects[0].Path != "/home/dev/inventory" {
		t.Fatalf("projeto estacionado conhecido desapareceu: %#v", effective.Projects)
	}
	if effective.EffectiveRoutePort(effective.Projects[0]) != 8089 {
		t.Fatalf("porta persistida não preservada: %d", effective.EffectiveRoutePort(effective.Projects[0]))
	}
}

func TestSetAuthNeverPersistsPlaintextWhenHashingFails(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	service := New(t.TempDir())
	service.Caddy = platform.CaddyClient{Runner: failingRunner{}, WSL: true}
	ctx := context.Background()
	if _, err := service.SetAuth(ctx, "default", true, "admin", "plain-secret"); !errors.Is(err, ErrPasswordHashUnavailable) {
		t.Fatalf("esperava falha de hashing segura, obtive %v", err)
	}
	cfg, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AuthUsers) != 0 {
		t.Fatalf("credencial foi persistida após falha de hashing: %#v", cfg.AuthUsers)
	}
}

func TestMigrateLegacyAuthIsAtomicAndHashesEveryCredential(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	service := New(t.TempDir())
	service.Caddy = platform.CaddyClient{Runner: hashingRunner{}, WSL: true}
	cfg := domain.NewConfig()
	cfg.AuthUsers = []domain.AuthUser{{Username: "global", PasswordHash: "legacy-global"}}
	cfg.Projects = []domain.Project{{Name: "site", Path: t.TempDir(), AuthUsers: []domain.AuthUser{{Username: "site", PasswordHash: "legacy-site"}}}}
	if err := service.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MigrateLegacyAuth(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.Config()
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range append(loaded.AuthUsers, loaded.Projects[0].AuthUsers...) {
		if !isCaddyPasswordHash(user.PasswordHash) || strings.Contains(user.PasswordHash, "legacy-") {
			t.Fatalf("credencial não migrada: %#v", user)
		}
	}

	service.Caddy = platform.CaddyClient{Runner: failingRunner{}, WSL: true}
	loaded.AuthUsers[0].PasswordHash = "still-plain"
	if err := service.Store.Save(loaded); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MigrateLegacyAuth(context.Background()); !errors.Is(err, ErrPasswordHashUnavailable) {
		t.Fatalf("esperava falha segura de migração: %v", err)
	}
	afterFailure, err := service.Config()
	if err != nil {
		t.Fatal(err)
	}
	if afterFailure.AuthUsers[0].PasswordHash != "still-plain" {
		t.Fatalf("migração parcial persistiu estado inesperado: %#v", afterFailure.AuthUsers)
	}
}

type failingRunner struct{}

func (failingRunner) Run(context.Context, ...string) (string, error) {
	return "", errors.New("caddy indisponível")
}

type hashingRunner struct{}

func (hashingRunner) Run(context.Context, ...string) (string, error) {
	return "$2a$10$characterization-hash", nil
}

func TestNewLoadsConfiguredWSLDistribution(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "wsl-distribution"), []byte("Ubuntu-24.04\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New(dataDir)
	if service.WSL.Distribution != "Ubuntu-24.04" {
		t.Fatalf("distribuição WSL não carregada: %q", service.WSL.Distribution)
	}
}

func TestIgnoreParkedProjectRemovesItFromEffectiveConfig(t *testing.T) {
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{
		Directories: map[string]bool{"/home/dev": true},
		Children:    map[string][]string{"/home/dev": {"/home/dev/dougdesign-seo"}},
		Files: map[string]bool{
			"/home/dev/dougdesign-seo/artisan":          true,
			"/home/dev/dougdesign-seo/public/index.php": true,
		},
	}}
	service.legacyWindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.legacyWSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	ctx := context.Background()

	if _, _, err := service.Park(ctx, "/home/dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.IgnoreProject(ctx, "dougdesign-seo"); err != nil {
		t.Fatalf("ignore parked project: %v", err)
	}

	cfg, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Parks) != 1 || len(cfg.Parks[0].IgnoredPaths) != 1 || cfg.Parks[0].IgnoredPaths[0] != "/home/dev/dougdesign-seo" {
		t.Fatalf("ignored project was not persisted: %#v", cfg.Parks)
	}
	effective, err := service.EffectiveConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(effective.Projects) != 0 {
		t.Fatalf("ignored project should not be discovered: %#v", effective.Projects)
	}

	if err := cfg.UnignoreParkProject("/home/dev/dougdesign-seo"); err != nil {
		t.Fatalf("unignore parked project: %v", err)
	}
	effective, err = service.EffectiveConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(effective.Projects) != 1 || effective.Projects[0].Name != "dougdesign-seo" {
		t.Fatalf("unignored project should be discovered again: %#v", effective.Projects)
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
	service.legacyWindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.legacyWSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
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
	service.legacyWindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.legacyWSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
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
	service.legacyWindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.legacyWSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
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
	service.legacyWindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	if err := service.Trust(context.Background()); err != nil {
		t.Fatalf("trust falhou: %v", err)
	}
}

func TestAppDoctorIncludesPortAndIPChecks(t *testing.T) {
	service := New(t.TempDir())
	service.legacyWindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.legacyWSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{
		Directories: map[string]bool{"/home/dev/financeiro": true},
		Files: map[string]bool{
			"/home/dev/financeiro/artisan":          true,
			"/home/dev/financeiro/public/index.php": true,
		},
	}}
	if _, _, err := service.Link(context.Background(), "financeiro", "/home/dev/financeiro"); err != nil {
		t.Fatal(err)
	}
	checks, err := service.Doctor(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	foundPortHTTP := false
	foundIP := false
	foundCA := false
	foundLocalName := false
	foundLANPort := false
	for _, check := range checks {
		if strings.HasPrefix(check.Name, "Porta HTTP") {
			foundPortHTTP = true
		}
		if check.Name == "IP LAN" {
			foundIP = true
		}
		if check.Name == "CA Local" {
			foundCA = true
		}
		if check.Name == "Projeto financeiro (Nome Local)" {
			foundLocalName = true
		}
		if check.Name == "Projeto financeiro (Porta LAN)" {
			foundLANPort = true
		}
	}
	if !foundPortHTTP {
		t.Fatalf("Doctor deveria incluir checagem de porta HTTP: %#v", checks)
	}
	if !foundIP {
		t.Fatalf("Doctor deveria incluir checagem de IP LAN: %#v", checks)
	}
	if !foundCA {
		t.Fatalf("Doctor deveria incluir checagem de CA Local: %#v", checks)
	}
	if !foundLocalName {
		t.Fatalf("Doctor deveria incluir checagem de Nome Local do projeto: %#v", checks)
	}
	if !foundLANPort {
		t.Fatalf("Doctor deveria incluir checagem de Porta LAN do projeto: %#v", checks)
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
	service.legacyWindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.legacyWSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}

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
	service.legacyWindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.legacyWSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}

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
	service.Caddy = platform.CaddyClient{Runner: hashingRunner{}, WSL: true}
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{
		Files: map[string]bool{
			"/home/dev/app/dist/index.html": true,
		},
	}}
	service.legacyWindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.legacyWSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}

	_, _, err := service.Link(ctx, "app", "/home/dev/app")
	if err != nil {
		t.Fatal(err)
	}

	// Test LAN port override
	port := 8088
	if _, err := service.SetRoutePort(ctx, "app", &port); err != nil {
		t.Fatalf("SetRoutePort: %v", err)
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
	if _, _, err := service.ExposeProject(ctx, "app", 10*time.Minute); err != nil {
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

	// Test Security Audit
	logs, err := service.SecurityAuditLogs(ctx, 10)
	if err != nil {
		t.Fatalf("SecurityAuditLogs: %v", err)
	}
	if !strings.Contains(logs, "ROUTE_PORT_CHANGE") {
		t.Fatalf("Audit log missing event: %s", logs)
	}
}

func TestRouteAllocationsPersistByPathAndPruneExplicitly(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	ctx := context.Background()
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{}}
	service.ExternalListeners = func(context.Context) ([]int, error) { return nil, nil }
	service.legacyWindowsCaddy = platform.CaddyClient{Runner: successfulRunner{}}
	service.legacyWSLCaddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	staticMode := domain.ModeStatic
	cfg := domain.NewConfig()
	cfg.RoutePortCount = 2
	cfg.Projects = []domain.Project{
		{Name: "zeta", Path: "/home/dev/zeta", Mode: &staticMode, StaticDir: stringPtr("dist")},
		{Name: "alpha", Path: "/home/dev/alpha", Mode: &staticMode, StaticDir: stringPtr("dist")},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveConfigAndApply(ctx, cfg, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RoutePortAllocations["/home/dev/alpha"] != 8080 || loaded.RoutePortAllocations["/home/dev/zeta"] != 8081 {
		t.Fatalf("alocações iniciais inesperadas: %#v", loaded.RoutePortAllocations)
	}

	// Reordering the input cannot move an existing path allocation.
	loaded.Projects[0], loaded.Projects[1] = loaded.Projects[1], loaded.Projects[0]
	if _, err := service.SaveConfigAndApply(ctx, loaded, false); err != nil {
		t.Fatal(err)
	}
	reordered, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reordered.RoutePortAllocations["/home/dev/alpha"] != 8080 || reordered.RoutePortAllocations["/home/dev/zeta"] != 8081 {
		t.Fatalf("reordenação alterou a alocação: %#v", reordered.RoutePortAllocations)
	}

	// Removing a project keeps its allocation as an orphan until explicit prune.
	reordered.Projects = reordered.Projects[:1]
	if _, err := service.SaveConfigAndApply(ctx, reordered, false); err != nil {
		t.Fatal(err)
	}
	allocations, err := service.RouteAllocations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(allocations) != 2 || allocations[0].Orphan || !allocations[1].Orphan || allocations[1].Path != "/home/dev/zeta" {
		t.Fatalf("órfão deveria ser apenas reportado: %#v", allocations)
	}
	preview, _, err := service.PruneRouteAllocations(ctx, true)
	if err != nil || len(preview) != 1 {
		t.Fatalf("dry-run inesperado: paths=%v err=%v", preview, err)
	}
	stillPresent, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stillPresent.RoutePortAllocations) != 2 {
		t.Fatalf("dry-run alterou o estado: %#v", stillPresent.RoutePortAllocations)
	}
	if _, _, err := service.PruneRouteAllocations(ctx, false); err != nil {
		t.Fatal(err)
	}
	pruned, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(pruned.RoutePortAllocations) != 1 {
		t.Fatalf("prune não removeu somente o órfão: %#v", pruned.RoutePortAllocations)
	}
}

func stringPtr(value string) *string { return &value }
