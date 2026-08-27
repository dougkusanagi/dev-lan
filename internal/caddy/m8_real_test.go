package caddy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// This opt-in test exercises the generated Caddyfile with a real Caddy
// binary. It is kept out of the default cross-platform suite because CI and
// Linux contributors are not required to install Caddy.
func TestRenderWSLUnifiedWithRealCaddy(t *testing.T) {
	if os.Getenv("DEVLAN_REAL_CADDY") != "1" {
		t.Skip("ative DEVLAN_REAL_CADDY=1 para validar com um binário Caddy instalado")
	}
	binary := os.Getenv("DEVLAN_CADDY_BIN")
	if binary == "" {
		resolved, err := exec.LookPath("caddy")
		if err != nil {
			t.Skip("binário Caddy não encontrado")
		}
		binary = resolved
	}
	cfg := domain.NewConfig()
	staticMode := domain.ModeStatic
	dist := "dist"
	cfg.Projects = []domain.Project{{Name: "static-app", Path: "/home/dev/static-app", Mode: &staticMode, StaticDir: &dist}}
	contents, err := RenderWSLUnifiedWithAccessLog(cfg, "/var/log/devlan/access.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Caddyfile")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "validate", "--config", path, "--adapter", "caddyfile")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Caddy rejeitou o Caddyfile unificado: %v\n%s\n%s", err, strings.TrimSpace(string(output)), contents)
	}
}

