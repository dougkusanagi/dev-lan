package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallManifestCapturesPreexistingRestoreBackup(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	shared := filepath.Join(dir, "shared.conf")
	if err := os.WriteFile(shared, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := store.EnsureInstallManifest([]ManifestResource{{
		ID: "shared.config", Scope: "shared", Kind: "file", Path: shared, Restore: true,
	}}, "Ubuntu-24.04")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Resources) != 1 || manifest.Resources[0].Ownership != OwnershipModified {
		t.Fatalf("proveniência inesperada: %#v", manifest.Resources)
	}
	backup := manifest.Resources[0].BackupPath
	if backup == "" {
		t.Fatal("backup do recurso compartilhado não foi registrado")
	}
	data, err := os.ReadFile(backup)
	if err != nil || string(data) != "before\n" {
		t.Fatalf("backup incorreto: %q, %v", data, err)
	}

	loaded, exists, err := store.LoadManifest()
	if err != nil || !exists || loaded.Resources[0].BeforeSHA256 == "" {
		t.Fatalf("manifesto não persistido: %#v, %t, %v", loaded, exists, err)
	}
}

func TestInstallManifestDoesNotDowngradeOwnership(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.EnsureInstallManifest([]ManifestResource{{ID: "resource", Scope: "wsl", Kind: "file", Ownership: OwnershipUnknown}}, "Ubuntu"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureInstallManifest([]ManifestResource{{ID: "resource", Scope: "wsl", Kind: "file", Ownership: OwnershipCreated}}, "Ubuntu"); err != nil {
		t.Fatal(err)
	}
	manifest, exists, err := store.LoadManifest()
	if err != nil || !exists {
		t.Fatal(err)
	}
	if manifest.Resources[0].Ownership != OwnershipCreated {
		t.Fatalf("ownership comprovada não foi atualizada: %#v", manifest.Resources[0])
	}
}

func TestInstallManifestUpgradesWSLMarkerEvidence(t *testing.T) {
	store := NewStore(t.TempDir())
	resource := ManifestResource{ID: "wsl.caddy-service", Scope: "wsl", Kind: "service", Ownership: OwnershipPreexisting}
	if _, err := store.EnsureInstallManifest([]ManifestResource{resource}, "Ubuntu"); err != nil {
		t.Fatal(err)
	}
	resource.Ownership = OwnershipCreated
	if _, err := store.EnsureInstallManifest([]ManifestResource{resource}, "Ubuntu"); err != nil {
		t.Fatal(err)
	}
	manifest, exists, err := store.LoadManifest()
	if err != nil || !exists {
		t.Fatal(err)
	}
	if manifest.Resources[0].Ownership != OwnershipCreated {
		t.Fatalf("evidência do marcador WSL não atualizou ownership: %#v", manifest.Resources[0])
	}
}
