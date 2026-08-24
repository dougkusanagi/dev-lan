package app

import (
	"context"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/detect"
)

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
