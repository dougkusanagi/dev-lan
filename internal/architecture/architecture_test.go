package architecture

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestRoutingModesCannotReturn is intentionally source-based. The routing
// decision is architectural, so a compile-only test would not catch someone
// reintroducing a field in a DTO, a CLI branch, or a generated Caddy template.
func TestRoutingModesCannotReturn(t *testing.T) {
	root := filepath.Join("..", "..")
	forbidden := []string{
		"Route" + "Mode",
		"Route" + "Host",
		"Default" + "RouteMode",
		"Domain" + "Suffix",
		"Parse" + "RouteMode",
		"Set" + "RouteMode",
		"Effective" + "RouteMode",
		"Effective" + "RouteHost",
		"Resolved" + "RouteMode",
		"route_" + "mode",
		"route_" + "host",
		"default_route_" + "mode",
		"domain_" + "suffix",
		"handle" + "_path",
		"Refer" + "er",
		"Hosts" + "Entries",
		"Sync" + "Hosts",
		"dns " + "entries",
		"dns " + "sync",
	}

	for _, directory := range []string{"cmd", "internal", filepath.Join("frontend", "src")} {
		directory := filepath.Join(root, directory)
		err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".go" && ext != ".ts" && ext != ".tsx" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(data)
			for _, token := range forbidden {
				if strings.Contains(text, token) {
					t.Errorf("símbolo de roteamento removido %q encontrado em %s", token, path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("inspecionar arquitetura em %s: %v", directory, err)
		}
	}
}

// TestTransportsUseApplicationBoundary guards R-05e. Transport packages may
// compose an App or a shell adapter at startup, but request/command handlers
// must not reach through it to coordinate persistence or runtime adapters.
func TestTransportsUseApplicationBoundary(t *testing.T) {
	root := filepath.Join("..", "..")
	directAdapter := regexp.MustCompile(`(?m)\b(?:service|a|s)\.(?:Store|WSL|PHP|Dev|Firewall|Telemetry|Detector|Caddy)\b`)
	directStore := regexp.MustCompile(`(?m)\bconfig\.Store\b`)
	for _, relative := range []string{"cmd/devlan", "internal/api", "internal/gui"} {
		directory := filepath.Join(root, relative)
		err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(data)
			if directAdapter.MatchString(text) {
				t.Errorf("transporte acessa adapter através de fachada em %s", path)
			}
			if directStore.MatchString(text) {
				t.Errorf("transporte acessa config.Store em %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("inspecionar transporte em %s: %v", relative, err)
		}
	}
}

// TestM8UnifiedEdgeContract guards the active production path during the
// migration window. Legacy artifacts remain readable for rollback, but new
// reloads, the unified renderer and the installer must not grow a second edge.
func TestM8UnifiedEdgeContract(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(relative string) string {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatalf("ler %s: %v", relative, err)
		}
		return string(data)
	}
	readPackage := func(relative string) string {
		entries, err := os.ReadDir(filepath.Join(root, relative))
		if err != nil {
			t.Fatalf("ler pacote %s: %v", relative, err)
		}
		var source strings.Builder
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			source.WriteString(read(filepath.Join(relative, entry.Name())))
			source.WriteByte('\n')
		}
		return source.String()
	}

	render := read(filepath.Join("internal", "caddy", "render.go"))
	start := strings.Index(render, "func RenderWSLUnified(")
	if start < 0 {
		t.Fatal("renderer unificado não encontrado")
	}
	activeRenderer := render[start:]
	for _, forbidden := range []string{"X-DevLAN-", "127.0.0.1:2019", "8181"} {
		if strings.Contains(activeRenderer, forbidden) {
			t.Fatalf("renderer unificado reintroduziu o contrato legado %q", forbidden)
		}
	}
	for _, required := range []string{"RenderWSLUnifiedWithAccessLog", "wslAdminAddress", "reverse_proxy 127.0.0.1:%d"} {
		if !strings.Contains(activeRenderer, required) {
			t.Fatalf("renderer unificado não contém %q", required)
		}
	}

	app := readPackage(filepath.Join("internal", "app"))
	applyStart := strings.Index(app, "func (a *App) apply(")
	if applyStart < 0 {
		t.Fatal("pipeline apply não encontrado")
	}
	applyEnd := strings.Index(app[applyStart:], "\nfunc ")
	if applyEnd < 0 {
		t.Fatal("fim do pipeline apply não encontrado")
	}
	activeApply := app[applyStart : applyStart+applyEnd]
	for _, required := range []string{"RenderWSLUnifiedWithAccessLog", "ApplyCaddy", "edgeCaddy"} {
		if !strings.Contains(activeApply, required) {
			t.Fatalf("pipeline apply não usa %q", required)
		}
	}
	if strings.Contains(activeApply, "RenderWindows(") || strings.Contains(activeApply, "ApplyGenerated(") {
		t.Fatal("pipeline apply ainda gera a topologia dupla")
	}

	installer := read(filepath.Join("scripts", "install.ps1"))
	for _, forbidden := range []string{"Install-WindowsCaddy", "Start-WindowsCaddy", "Caddyfile.windows", "127.0.0.1:2019"} {
		if strings.Contains(installer, forbidden) {
			t.Fatalf("instalador reintroduziu a borda Windows %q", forbidden)
		}
	}
}
