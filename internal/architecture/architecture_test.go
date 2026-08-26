package architecture

import (
	"os"
	"path/filepath"
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
