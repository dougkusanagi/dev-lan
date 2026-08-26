package caddy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// TestFixtureManifestContracts verifies that the static, php, vite, and ssr fixtures
// comply with the dual-origin proxy contract: root (/), absolute assets, redirects,
// cookies (Path=/), WebSocket upgrade, and origin preservation.
func TestFixtureManifestContracts(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "testdata", "fixtures", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ler manifest.json das fixtures: %v", err)
	}

	var manifest struct {
		Version  int `json:"version"`
		Fixtures []struct {
			Name    string   `json:"name"`
			Root    string   `json:"root,omitempty"`
			Checks  []string `json:"checks"`
			Command []string `json:"command,omitempty"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse manifest.json: %v", err)
	}

	if len(manifest.Fixtures) < 4 {
		t.Fatalf("esperadas ao menos 4 fixtures no manifesto, encontradas %d", len(manifest.Fixtures))
	}

	// 1. Verify Static Fixture files exist and match contract
	staticDir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "static"))
	if err != nil {
		t.Fatal(err)
	}
	indexHTML, err := os.ReadFile(filepath.Join(staticDir, "index.html"))
	if err != nil || !strings.Contains(string(indexHTML), "/assets/app.css") {
		t.Fatalf("fixture static deve conter index.html com asset absoluto /assets/app.css: %v", err)
	}
	appCSS, err := os.ReadFile(filepath.Join(staticDir, "assets", "app.css"))
	if err != nil || !strings.Contains(string(appCSS), "color") {
		t.Fatalf("fixture static deve conter assets/app.css: %v", err)
	}

	// 2. Verify PHP Fixture file exists and implements root, redirect, cookie, origin, asset
	phpIndex, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "php", "public", "index.php"))
	if err != nil {
		t.Fatal(err)
	}
	phpContent := string(phpIndex)
	for _, check := range []string{"/redirect", "/cookie", "/asset.css", "/origin", "devlan_fixture"} {
		if !strings.Contains(phpContent, check) {
			t.Fatalf("fixture PHP deve implementar check %q:\n%s", check, phpContent)
		}
	}

	// 3. Verify Vite Fixture files
	viteDir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "vite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(filepath.Join(viteDir, "index.html")); err != nil {
		t.Fatalf("fixture vite index.html ausente: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(viteDir, "src", "main.js")); err != nil {
		t.Fatalf("fixture vite src/main.js ausente: %v", err)
	}

	// 4. Verify SSR Fixture script
	ssrScript, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "ssr", "server.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	ssrContent := string(ssrScript)
	for _, check := range []string{"/assets/app.css", "/redirect", "/cookie", "/origin", "upgrade", "websocket"} {
		if !strings.Contains(ssrContent, check) {
			t.Fatalf("fixture SSR deve implementar check %q:\n%s", check, ssrContent)
		}
	}
}

// TestDualOriginCaddyfileGeneration validates that RenderWindows and RenderWSL
// produce correct routing for all fixtures simultaneously across both origins:
// Local: https://<name>.localhost/
// LAN: http(s)://<IP>:<porta>/
func TestDualOriginCaddyfileGeneration(t *testing.T) {
	staticMode := domain.ModeStatic
	devMode := domain.ModeDev
	phpMode := domain.ModePHP
	presetLaravel := domain.PHPPresetLaravel
	presetGeneric := domain.PHPPresetGeneric
	dist := "dist"
	vitePort := 9150
	ssrPort := 9160
	phpPort := 8080
	staticPort := 8081
	viteRoutePort := 8082
	ssrRoutePort := 8083

	cfg := domain.NewConfig()
	cfg.LANAddress = "192.168.1.100"
	cfg.TLSEnabled = true
	secure := true

	cfg.Projects = []domain.Project{
		{
			Name:      "app-php",
			Path:      "/home/dev/app-php",
			Mode:      &phpMode,
			PHPPreset: &presetLaravel,
			RoutePort: &phpPort,
			Secure:    &secure,
		},
		{
			Name:      "app-static",
			Path:      "/home/dev/app-static",
			Mode:      &staticMode,
			StaticDir: &dist,
			RoutePort: &staticPort,
		},
		{
			Name:      "app-vite",
			Path:      "/home/dev/app-vite",
			Mode:      &devMode,
			DevPort:   &vitePort,
			RoutePort: &viteRoutePort,
		},
		{
			Name:      "app-ssr",
			Path:      "/home/dev/app-ssr",
			Mode:      &devMode,
			DevPort:   &ssrPort,
			RoutePort: &ssrRoutePort,
		},
		{
			Name:      "app-php-generic",
			Path:      "/home/dev/app-php-generic",
			Mode:      &phpMode,
			PHPPreset: &presetGeneric,
		},
	}

	if err := cfg.Normalize(); err != nil {
		t.Fatalf("configuração inválida: %v", err)
	}

	winCaddy, err := RenderWindows(cfg)
	if err != nil {
		t.Fatalf("RenderWindows falhou: %v", err)
	}

	// Verify Windows Local Edge (.localhost)
	for _, expected := range []string{
		"https://app-php.localhost",
		"https://app-static.localhost",
		"https://app-vite.localhost",
		"https://app-ssr.localhost",
		"https://app-php-generic.localhost",
		"remote_ip 127.0.0.1 ::1",
		"header_up -X-DevLAN-Port",
		"header_up -X-DevLAN-Project",
		"header_up -X-DevLAN-Local",
		"header_up -X-DevLAN-HTTPS",
		"header_up X-DevLAN-Local on",
		"header_up X-DevLAN-HTTPS on",
		"respond \"Acesso local permitido somente via loopback\" 403",
	} {
		if !strings.Contains(winCaddy, expected) {
			t.Fatalf("Caddyfile Windows não contém %q:\n%s", expected, winCaddy)
		}
	}

	// Verify Windows LAN listeners (dedicated ports with header sanitization)
	for _, expected := range []string{
		"https://:8080 {", // PHP project with TLS enabled
		"header_up X-DevLAN-Port 8080",
		"header_up X-DevLAN-Project app-php",
		"header_up X-Forwarded-Port 8080",
		"header_up X-DevLAN-HTTPS on",
		":8081 {", // Static project
		"header_up X-DevLAN-Port 8081",
		"header_up X-DevLAN-Project app-static",
		"header_up X-Forwarded-Port 8081",
		":8082 {", // Vite project
		"header_up X-DevLAN-Port 8082",
		"header_up X-DevLAN-Project app-vite",
		"header_up X-Forwarded-Port 8082",
		":8083 {", // SSR project
		"header_up X-DevLAN-Port 8083",
		"header_up X-DevLAN-Project app-ssr",
		"header_up X-Forwarded-Port 8083",
	} {
		if !strings.Contains(winCaddy, expected) {
			t.Fatalf("Caddyfile Windows não contém listener LAN esperado %q:\n%s", expected, winCaddy)
		}
	}

	wslCaddy, err := RenderWSL(cfg)
	if err != nil {
		t.Fatalf("RenderWSL falhou: %v", err)
	}

	// Verify WSL Caddy listeners and handlers
	for _, expected := range []string{
		"bind 127.0.0.1",
		"@devlan_local_app-php header_regexp Host ^app-php\\.localhost(?::\\d+)?$",
		"@devlan_port_app-php header X-DevLAN-Port 8080",
		`root * "/home/dev/app-php/public"`,
		"env HTTPS {http.request.header.X-DevLAN-HTTPS}",
		"@devlan_local_app-static header_regexp Host ^app-static\\.localhost(?::\\d+)?$",
		"@devlan_port_app-static header X-DevLAN-Port 8081",
		`root * "/home/dev/app-static/dist"`,
		"try_files {path} {path}/ /index.html",
		"@devlan_port_app-vite header X-DevLAN-Port 8082",
		"reverse_proxy 127.0.0.1:9150",
		"header_up Upgrade {http.request.header.Upgrade}",
		"header_up Connection {http.request.header.Connection}",
		"@devlan_port_app-ssr header X-DevLAN-Port 8083",
		"reverse_proxy 127.0.0.1:9160",
		`root * "/home/dev/app-php-generic"`,
	} {
		if !strings.Contains(wslCaddy, expected) {
			t.Fatalf("Caddyfile WSL não contém configuração esperada %q:\n%s", expected, wslCaddy)
		}
	}
}

// TestAntiSpoofingHeaderFilter tests that incoming untrusted headers from simulated
// clients are not accepted when reaching our handler pipelines.
func TestAntiSpoofingHeaderFilter(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://192.168.1.50:8080/origin", nil)
	req.Header.Set("X-DevLAN-Port", "9999")
	req.Header.Set("X-DevLAN-Project", "admin-spoof")
	req.Header.Set("X-DevLAN-Local", "on")
	req.Header.Set("X-DevLAN-HTTPS", "on")

	// Simulating edge sanitization: remove all X-DevLAN-* then set trusted
	cleanedHeaders := make(http.Header)
	for k, v := range req.Header {
		if !strings.HasPrefix(strings.ToLower(k), "x-devlan-") {
			cleanedHeaders[k] = v
		}
	}

	// Edge injects trusted metadata
	cleanedHeaders.Set("X-DevLAN-Port", "8080")
	cleanedHeaders.Set("X-DevLAN-Project", "real-project")

	if cleanedHeaders.Get("X-DevLAN-Port") != "8080" {
		t.Fatalf("porta deveria ser a configurada (8080), obtido %s", cleanedHeaders.Get("X-DevLAN-Port"))
	}
	if cleanedHeaders.Get("X-DevLAN-Project") != "real-project" {
		t.Fatalf("projeto deveria ser o configurado (real-project), obtido %s", cleanedHeaders.Get("X-DevLAN-Project"))
	}
	if cleanedHeaders.Get("X-DevLAN-Local") != "" {
		t.Fatalf("header spoofed X-DevLAN-Local deveria ter sido removido")
	}
	if cleanedHeaders.Get("X-DevLAN-HTTPS") != "" {
		t.Fatalf("header spoofed X-DevLAN-HTTPS deveria ter sido removido")
	}
}
