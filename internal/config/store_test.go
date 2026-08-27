package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

func TestStoreUsesOptimisticRevisionAndRecoversInterruptedPair(t *testing.T) {
	store := NewStore(t.TempDir())
	first := domain.NewConfig()
	first.LANAddress = "192.168.1.10"
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	left, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	right, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	left.LANAddress = "192.168.1.11"
	if err := store.Save(left); err != nil {
		t.Fatal(err)
	}
	right.LANAddress = "192.168.1.12"
	if err := store.Save(right); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("esperava conflito de revisão, obtido %v", err)
	}

	next, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	next.LANAddress = "192.168.1.20"
	store.Fault = func(point string) error {
		if point == "rename.state" {
			return errors.New("falha simulada")
		}
		return nil
	}
	if err := store.Save(next); err == nil {
		t.Fatal("falha de rename deveria interromper o commit")
	}
	store.Fault = nil
	recovered, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if recovered.LANAddress != "192.168.1.11" {
		t.Fatalf("par anterior não restaurado: %#v", recovered)
	}
}

func TestStoreLockIsSharedAcrossProcesses(t *testing.T) {
	store := NewStore(t.TempDir())
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = store.WithLock(context.Background(), func() error { close(started); <-release; return nil })
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := store.WithLock(ctx, func() error { return nil }); err == nil {
		t.Fatal("segundo lock deveria aguardar e falhar por timeout")
	}
	close(release)
}

func TestStoreRoundTripAndTOML(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	cfg := domain.NewConfig()
	cfg.LANAddress = "192.168.1.50"
	cfg.DefaultMode = domain.ModePHP
	cfg.TLSEnabled = true
	cfg.Projects = []domain.Project{{Name: "financeiro", Path: "/home/dev/financeiro"}}
	cfg.Parks = []domain.Park{{Path: "/home/dev"}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `lan_address = "192.168.1.50"`) {
		t.Fatalf("TOML não contém endereço: %s", data)
	}
	if !strings.Contains(string(data), "tls_enabled = true") || !strings.Contains(string(data), "https_port = 443") {
		t.Fatalf("TOML não contém configuração TLS: %s", data)
	}
	for _, legacyKey := range []string{"default_route_mode", "domain_suffix", "route_mode", "route_host"} {
		if strings.Contains(string(data), legacyKey) {
			t.Fatalf("TOML contém chave de roteamento removida %q: %s", legacyKey, data)
		}
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.TLSEnabled || loaded.HTTPSPort != 443 || len(loaded.Projects) != 1 || loaded.Projects[0].Name != "financeiro" || len(loaded.Parks) != 1 {
		t.Fatalf("round-trip incorreto: %#v", loaded)
	}
}

func TestStoreRejectsStateAndConfigFromRemovedRoutingSchema(t *testing.T) {
	t.Run("config TOML", func(t *testing.T) {
		store := NewStore(t.TempDir())
		if err := os.WriteFile(store.Paths().Config, []byte("default_route_mode = \"path\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil {
			t.Fatal("configuração com default_route_mode removido deveria ser rejeitada")
		}
	})

	t.Run("estado JSON", func(t *testing.T) {
		store := NewStore(t.TempDir())
		state := `{"version":1,"schema_version":1,"revision":1,"projects":[{"name":"portal","path":"/home/dev/portal","route_mode":"path"}],"parks":[]}`
		if err := os.WriteFile(store.Paths().State, []byte(state), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil {
			t.Fatal("estado com route_mode removido deveria ser rejeitado")
		}
	})
}

func TestStoreRoundTripPHPVersionsPoolsAndProjectOverrides(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := domain.NewConfig()
	if _, err := cfg.AddPHPVersion("8.3", []string{"xml", "mbstring"}); err != nil {
		t.Fatal(err)
	}
	version := "8.3"
	isolated := true
	preset := domain.PHPPresetSymfony
	cfg.Projects = []domain.Project{{Name: "portal", Path: "/home/dev/portal", PHPVersion: &version, PHPIsolatedPool: &isolated, PHPPreset: &preset}}
	cfg.Composer.Environment = domain.ComposerSystem
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.PHPVersions) != 1 || loaded.PHPVersions[0].Version != "8.3" || loaded.Composer.Environment != domain.ComposerSystem {
		t.Fatalf("versões PHP não persistidas: %#v", loaded)
	}
	project, found := loaded.Project("portal")
	if !found || project.PHPVersion == nil || *project.PHPVersion != "8.3" || project.PHPIsolatedPool == nil || !*project.PHPIsolatedPool || project.PHPPreset == nil || *project.PHPPreset != domain.PHPPresetSymfony {
		t.Fatalf("override PHP não persistido: %#v", loaded.Projects)
	}
}

func TestApplyGeneratedLeavesLastGoodFilesWhenValidationFails(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.ApplyGenerated("old windows", "old wsl", nil); err != nil {
		t.Fatal(err)
	}
	err := store.ApplyGenerated("new windows", "new wsl", func(_, _ string) error {
		return errors.New("configuração inválida")
	})
	if err == nil {
		t.Fatal("validação deveria falhar")
	}
	windows, wsl, err := store.Generated()
	if err != nil {
		t.Fatal(err)
	}
	if windows != "old windows" || wsl != "old wsl" {
		t.Fatalf("última configuração funcional foi alterada: %q / %q", windows, wsl)
	}
}

func TestRollbackRemovesFirstGeneration(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.ApplyGenerated("first windows", "first wsl", nil); err != nil {
		t.Fatal(err)
	}
	if err := store.RollbackGenerated(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Generated(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback inicial deveria remover arquivos: %v", err)
	}
}

func TestRollbackPHPFilesRestoresLastGeneratedPools(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.ApplyPHPFiles(map[string]string{"php-8-3.conf": "old"}, "old info"); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyPHPFiles(map[string]string{"php-8-5.conf": "new"}, "new info"); err != nil {
		t.Fatal(err)
	}
	if err := store.RollbackPHPFiles(); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(filepath.Join(store.Paths().PHPGeneratedDir, "php-8-3.conf"))
	if err != nil || string(old) != "old" {
		t.Fatalf("pool PHP antigo não restaurado: %q, %v", old, err)
	}
	if _, err := os.Stat(filepath.Join(store.Paths().PHPGeneratedDir, "php-8-5.conf")); !os.IsNotExist(err) {
		t.Fatalf("pool PHP novo deveria ser removido: %v", err)
	}
}

func TestStoreRoundTripPhase4FieldsAndAudit(t *testing.T) {
	store := NewStore(t.TempDir())
	cfg := domain.NewConfig()
	port := 8090
	cfg.RouteBasePort = 8080
	cfg.Allowlist = []string{"192.168.1.0/24"}
	cfg.AuthUsers = []domain.AuthUser{{Username: "admin", PasswordHash: "secret_hash"}}
	cfg.Projects = []domain.Project{
		{
			Name:         "spa",
			Path:         "/home/dev/spa",
			RoutePort:    &port,
			Allowlist:    []string{"10.0.0.0/8"},
			ExposedUntil: func() *string { s := "2029-01-01T00:00:00Z"; return &s }(),
		},
	}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Allowlist) != 1 || loaded.Allowlist[0] != "192.168.1.0/24" {
		t.Fatalf("allowlist global não persistida: %#v", loaded.Allowlist)
	}
	if len(loaded.AuthUsers) != 1 || loaded.AuthUsers[0].Username != "admin" {
		t.Fatalf("auth users globais não persistidos: %#v", loaded.AuthUsers)
	}
	p, found := loaded.Project("spa")
	if !found || p.RoutePort == nil || *p.RoutePort != 8090 {
		t.Fatalf("configuração de rota do projeto não persistida: %#v", p)
	}
	if len(p.Allowlist) != 1 || p.Allowlist[0] != "10.0.0.0/8" {
		t.Fatalf("allowlist do projeto não persistida: %#v", p.Allowlist)
	}

	// Test security audit logging
	if err := store.AppendSecurityAudit("TLS_ENABLE", "project=spa"); err != nil {
		t.Fatal(err)
	}
	audit, err := store.ReadSecurityAudit(10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(audit, "EVENT=TLS_ENABLE project=spa") {
		t.Fatalf("log de auditoria não contém evento esperado: %s", audit)
	}
}

func TestExportRemovesCredentialsAndTemporaryExposure(t *testing.T) {
	store := NewStore(t.TempDir())
	mode := domain.ModePHP
	auth := true
	exposed := "2030-01-01T00:00:00Z"
	cfg := domain.NewConfig()
	cfg.AuthUsers = []domain.AuthUser{{Username: "admin", PasswordHash: "$2a$secret"}}
	cfg.Projects = []domain.Project{{
		Name:         "portal",
		Path:         "/home/dev/portal",
		Mode:         &mode,
		AuthEnabled:  &auth,
		AuthUsers:    []domain.AuthUser{{Username: "user", PasswordHash: "project-hash"}},
		ExposedUntil: &exposed,
	}}
	data, err := MarshalExport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"$2a$secret", "project-hash", "admin", "2030-01-01"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("exportação contém dado sensível %q: %s", secret, data)
		}
	}
	imported, err := UnmarshalExport(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.AuthUsers) != 0 || len(imported.Projects[0].AuthUsers) != 0 || imported.Projects[0].AuthEnabled != nil || imported.Projects[0].ExposedUntil != nil {
		t.Fatalf("dados sensíveis não foram removidos no import: %#v", imported)
	}
	if err := store.Save(imported); err != nil {
		t.Fatal(err)
	}
}
