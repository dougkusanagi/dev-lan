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
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Projects) != 1 || loaded.Projects[0].Name != "financeiro" || len(loaded.Parks) != 1 {
		t.Fatalf("round-trip incorreto: %#v", loaded)
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
