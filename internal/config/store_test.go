package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

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
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.TLSEnabled || loaded.HTTPSPort != 443 || len(loaded.Projects) != 1 || loaded.Projects[0].Name != "financeiro" || len(loaded.Parks) != 1 {
		t.Fatalf("round-trip incorreto: %#v", loaded)
	}
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
