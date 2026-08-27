package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/config"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

type uninstallFirewall struct{}

func (uninstallFirewall) Ensure(context.Context, ...int) error { return nil }
func (uninstallFirewall) Remove(context.Context) error         { return nil }

func TestUninstallDryRunDoesNotRemoveDataOrProjects(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	dir := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := New(dir)
	service.WSLConfigPath = filepath.Join(dir, "wslconfig")
	service.WSL.Invoker = successfulRunner{}
	service.Firewall = uninstallFirewall{}
	if err := service.Store.Save(domain.NewConfig()); err != nil {
		t.Fatal(err)
	}
	if err := service.ensureInstallationManifest(context.Background()); err != nil {
		t.Fatal(err)
	}
	plan, err := service.PlanUninstall(context.Background(), UninstallOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Manifest || len(plan.Items) == 0 {
		t.Fatalf("plano incompleto: %#v", plan)
	}
	if _, err := os.Stat(service.Store.Paths().Config); err != nil {
		t.Fatalf("dry-run removeu configuração: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "index.html")); err != nil {
		t.Fatalf("dry-run alterou projeto: %v", err)
	}
}

func TestUninstallRemovesManagedDataAndPreservesProject(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	dir := t.TempDir()
	project := t.TempDir()
	service := New(dir)
	service.WSLConfigPath = filepath.Join(dir, "wslconfig")
	service.WSL.Invoker = successfulRunner{}
	service.Firewall = uninstallFirewall{}
	service.Caddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	if err := service.Store.Save(domain.NewConfig()); err != nil {
		t.Fatal(err)
	}
	if err := service.ensureInstallationManifest(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := service.UninstallWithOptions(context.Background(), UninstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(service.Store.Paths().Config); !os.IsNotExist(err) {
		t.Fatalf("configuração gerenciada deveria ser removida: %v", err)
	}
	if _, err := os.Stat(project); err != nil {
		t.Fatalf("diretório de projeto não deveria ser tocado: %v", err)
	}
	if result.Plan.ProjectCount != 0 || len(result.Plan.Items) == 0 {
		t.Fatalf("resultado sem plano verificável: %#v", result)
	}
}

func TestPlanUninstallRestoresManagedSharedConfig(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	dir := t.TempDir()
	shared := filepath.Join(dir, "wslconfig")
	before := []byte("[wsl2]\nmemory=4GB\n")
	after := []byte("[wsl2]\nmemory=8GB\n")
	if err := os.WriteFile(shared, after, 0o644); err != nil {
		t.Fatal(err)
	}
	service := New(dir)
	service.WSLConfigPath = shared
	backup := filepath.Join(dir, "before.wslconfig")
	if err := os.WriteFile(backup, before, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.Store.SaveManifest(config.InstallManifest{
		Version: config.InstallManifestVersion,
		Resources: []config.ManifestResource{{
			ID:            "windows.wslconfig",
			Scope:         "shared",
			Kind:          "file",
			Path:          shared,
			Ownership:     config.OwnershipModified,
			BeforeSHA256:  "before",
			ManagedSHA256: "managed",
			BackupPath:    backup,
			Restore:       true,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	// Use the real fingerprints so the three-way comparison is exercised.
	beforeHash, _ := config.FileSHA256(backup)
	afterHash, _ := config.FileSHA256(shared)
	manifest, _, _ := service.Store.LoadManifest()
	manifest.Resources[0].BeforeSHA256 = beforeHash
	manifest.Resources[0].ManagedSHA256 = afterHash
	if err := service.Store.SaveManifest(manifest); err != nil {
		t.Fatal(err)
	}
	plan, err := service.PlanUninstall(context.Background(), UninstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range plan.Items {
		if item.ID == "windows.wslconfig" {
			if item.Action != UninstallRestore {
				t.Fatalf("configuração compartilhada deveria ser restaurada: %#v", item)
			}
			return
		}
	}
	t.Fatal("item windows.wslconfig ausente do plano")
}
