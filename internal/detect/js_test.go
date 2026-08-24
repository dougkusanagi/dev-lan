package detect

import (
	"context"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

func TestDetectJSViteWithPnpm(t *testing.T) {
	ctx := context.Background()
	inspector := StaticInspector{
		Files: map[string]bool{
			"/home/dev/vite-app/package.json":    true,
			"/home/dev/vite-app/pnpm-lock.yaml":  true,
			"/home/dev/vite-app/vite.config.ts":  true,
			"/home/dev/vite-app/dist/index.html": true,
		},
		FileContents: map[string]string{
			"/home/dev/vite-app/package.json": `{
				"name": "vite-app",
				"scripts": {
					"dev": "vite",
					"build": "vite build"
				},
				"devDependencies": {
					"vite": "^5.0.0"
				}
			}`,
		},
	}
	detector := Detector{Inspector: inspector}
	res, err := detector.DetectJS(ctx, "/home/dev/vite-app")
	if err != nil {
		t.Fatalf("falha na detecção Vite: %v", err)
	}
	if res.Framework != "vite" {
		t.Fatalf("framework inesperado: %s", res.Framework)
	}
	if res.PackageManager != "pnpm" {
		t.Fatalf("package manager inesperado: %s", res.PackageManager)
	}
	if !res.HasDevServer || res.DevScript != "pnpm run dev" {
		t.Fatalf("script dev inesperado: %s", res.DevScript)
	}
	if res.StaticDir != "dist" {
		t.Fatalf("static dir inesperado: %s", res.StaticDir)
	}
	if !res.IsSPA {
		t.Fatal("deveria ser detectado como SPA")
	}

	detected, err := detector.DetectProject(ctx, "/home/dev/vite-app")
	if err != nil {
		t.Fatal(err)
	}
	if detected.Kind != ProjectKindDev || detected.SuggestedMode != domain.ModeDev {
		t.Fatalf("modo sugerido inesperado: %v, %s", detected.Kind, detected.SuggestedMode)
	}
}

func TestDetectJSNextWithYarn(t *testing.T) {
	ctx := context.Background()
	inspector := StaticInspector{
		Files: map[string]bool{
			"/home/dev/next-app/package.json":   true,
			"/home/dev/next-app/yarn.lock":      true,
			"/home/dev/next-app/next.config.js": true,
		},
		FileContents: map[string]string{
			"/home/dev/next-app/package.json": `{
				"name": "next-app",
				"scripts": {
					"dev": "next dev",
					"build": "next build"
				},
				"dependencies": {
					"next": "14.0.0",
					"react": "18.0.0"
				}
			}`,
		},
	}
	detector := Detector{Inspector: inspector}
	res, err := detector.DetectJS(ctx, "/home/dev/next-app")
	if err != nil {
		t.Fatal(err)
	}
	if res.Framework != "next" || res.PackageManager != "yarn" {
		t.Fatalf("detecção incorreta Next/Yarn: %#v", res)
	}
	if res.DevScript != "yarn dev" {
		t.Fatalf("comando dev inesperado: %s", res.DevScript)
	}
}

func TestDetectStaticOnly(t *testing.T) {
	ctx := context.Background()
	inspector := StaticInspector{
		Files: map[string]bool{
			"/home/dev/static-site/dist/index.html": true,
		},
	}
	detector := Detector{Inspector: inspector}
	detected, err := detector.DetectProject(ctx, "/home/dev/static-site")
	if err != nil {
		t.Fatal(err)
	}
	if detected.Kind != ProjectKindStatic || detected.SuggestedMode != domain.ModeStatic {
		t.Fatalf("projeto puramente estático deveria sugerir ModeStatic: %#v", detected)
	}
}

func TestRecommendRouteMode(t *testing.T) {
	nextProject := DetectedProject{
		Kind: ProjectKindDev,
		JS:   JSResult{Framework: "next", HasDevServer: true},
	}
	rec := RecommendRouteMode(nextProject)
	if rec.RecommendedMode != domain.RouteModePort {
		t.Fatalf("Next.js deveria recomendar RouteModePort: %#v", rec)
	}

	phpProject := DetectedProject{
		Kind: ProjectKindPHP,
		PHP:  PHPResult{Preset: domain.PHPPresetLaravel},
	}
	recPHP := RecommendRouteMode(phpProject)
	if recPHP.RecommendedMode != domain.RouteModePath {
		t.Fatalf("PHP deveria recomendar RouteModePath: %#v", recPHP)
	}
}
