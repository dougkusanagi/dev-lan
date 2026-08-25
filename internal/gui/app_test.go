package gui_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/detect"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/gui"
)

func setupTestApp(t *testing.T) (*gui.App, string) {
	t.Helper()
	tempDir := t.TempDir()
	os.Setenv("DEVLAN_TEST_MOCK", "1")

	service := app.New(tempDir)
	cfg := domain.NewConfig()
	cfg.LANAddress = "192.168.1.50"
	cfg.WindowsPort = 80
	cfg.HTTPSPort = 443
	cfg.TLSEnabled = true

	projPath := filepath.ToSlash(filepath.Join(tempDir, "sample-project"))
	if err := os.MkdirAll(filepath.Join(tempDir, "sample-project", "public"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "sample-project", "artisan"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "sample-project", "public", "index.php"), []byte("<?php"), 0644); err != nil {
		t.Fatal(err)
	}

	mode := domain.ModePHP
	cfg.Projects = append(cfg.Projects, domain.Project{
		Name: "sample-project",
		Path: projPath,
		Mode: &mode,
	})

	if err := service.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}

	guiApp := gui.NewApp(service)
	guiApp.Startup(context.Background())
	return guiApp, tempDir
}

func TestGUI_GetProjects(t *testing.T) {
	guiApp, _ := setupTestApp(t)

	projects, err := guiApp.GetProjects("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].Name != "sample-project" {
		t.Errorf("expected sample-project, got %s", projects[0].Name)
	}
	if projects[0].EffectiveMode != "php" {
		t.Errorf("expected effective mode php, got %s", projects[0].EffectiveMode)
	}
	if projects[0].Status != "ready" {
		t.Errorf("expected status ready, got %s", projects[0].Status)
	}
}

func TestGUI_GetProjectsMarksDiscoveredParkedProjectAsParked(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	tempDir := t.TempDir()
	service := app.New(tempDir)
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{
		Directories: map[string]bool{"/home/dev": true},
		Children:    map[string][]string{"/home/dev": {"/home/dev/dougdesign-seo"}},
		Files: map[string]bool{
			"/home/dev/dougdesign-seo/artisan":          true,
			"/home/dev/dougdesign-seo/public/index.php": true,
		},
	}}

	cfg := domain.NewConfig()
	cfg.Parks = []domain.Park{{Path: "/home/dev"}}
	if err := service.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}

	projects, err := gui.NewApp(service).GetProjects("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "dougdesign-seo" {
		t.Fatalf("unexpected projects: %#v", projects)
	}
	if projects[0].Kind != "parked" {
		t.Fatalf("discovered project should be parked, got %q", projects[0].Kind)
	}
}

func TestGUI_GetStatus(t *testing.T) {
	guiApp, _ := setupTestApp(t)

	status, err := guiApp.GetStatus()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.LANIP != "192.168.1.50" {
		t.Errorf("expected LAN IP 192.168.1.50, got %s", status.LANIP)
	}
	if status.WindowsPort != 80 {
		t.Errorf("expected port 80, got %d", status.WindowsPort)
	}
}

func TestGUI_GlobalConfig(t *testing.T) {
	guiApp, _ := setupTestApp(t)

	cfgView, err := guiApp.GetGlobalConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfgView.WindowsPort = 8088
	cfgView.DefaultMode = "dev"
	if err := guiApp.SaveGlobalConfig(cfgView); err != nil {
		t.Fatalf("unexpected error saving config: %v", err)
	}

	updated, err := guiApp.GetGlobalConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.WindowsPort != 8088 {
		t.Errorf("expected port 8088, got %d", updated.WindowsPort)
	}
	if updated.DefaultMode != "dev" {
		t.Errorf("expected mode dev, got %s", updated.DefaultMode)
	}
}

func TestGUI_ProjectConfigUpdate(t *testing.T) {
	guiApp, _ := setupTestApp(t)

	enabled := true
	update := gui.ProjectConfigUpdate{
		Name:       "sample-project",
		Mode:       "static",
		StaticDir:  "dist",
		TLSEnabled: &enabled,
	}

	if err := guiApp.SaveProjectConfig(update); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	projects, err := guiApp.GetProjects("sample-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].EffectiveMode != "static" {
		t.Errorf("expected static mode, got %s", projects[0].EffectiveMode)
	}
	if projects[0].StaticDir != "dist" {
		t.Errorf("expected staticDir dist, got %s", projects[0].StaticDir)
	}
}

func TestGUI_DoctorAndFix(t *testing.T) {
	guiApp, _ := setupTestApp(t)

	checks, err := guiApp.RunDoctor("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(checks) == 0 {
		t.Fatal("expected doctor checks, got empty list")
	}

	if err := guiApp.ApplyDoctorFix("reload", ""); err != nil {
		t.Fatalf("unexpected error applying reload fix: %v", err)
	}
}
