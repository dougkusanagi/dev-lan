package platform

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

func TestViteCommandUsesRequestedPort(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "npm script",
			command: "npm run dev",
			want:    "npm run dev -- --host 0.0.0.0 --port 19107",
		},
		{
			name:    "existing npm arguments",
			command: "npm run dev -- --https",
			want:    "npm run dev -- --https --host 0.0.0.0 --port 19107",
		},
		{
			name:    "explicit port preserved",
			command: "npm run dev -- --port 5173",
			want:    "npm run dev -- --port 5173",
		},
		{
			name:    "direct vite command",
			command: "vite",
			want:    "vite --host 0.0.0.0 --port 19107",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := viteCommand(test.command, 19107); got != test.want {
				t.Fatalf("viteCommand() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDevUnitName(t *testing.T) {
	if got, want := devUnitName("cj-spec-sheet"), "devlan-dev-cj-spec-sheet"; got != want {
		t.Fatalf("devUnitName() = %q, want %q", got, want)
	}
	if got, want := devUnitName("My Project/One"), "devlan-dev-my-project-one"; got != want {
		t.Fatalf("devUnitName() = %q, want %q", got, want)
	}
}

func TestUsesViteDetectsConfigOrFramework(t *testing.T) {
	manager := NewWSLDevManager(WSLRunner{})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vite.config.ts"), []byte("export default {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !manager.usesVite(context.Background(), domain.Project{Path: dir}) {
		t.Fatal("vite.config.ts deveria ser detectado")
	}

	framework := "vite"
	if !manager.usesVite(context.Background(), domain.Project{Path: t.TempDir(), DevFramework: &framework}) {
		t.Fatal("framework vite deveria ser detectado")
	}
}

func TestUsesViteDetectsWSLConfig(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("teste específico do WSL no Windows")
	}

	manager := NewWSLDevManager(NewWSLRunner("wsl.exe", ""))
	if _, err := manager.WSL.Run(context.Background(), "/usr/bin/test", "-f", "/home/silver/Sites/cj-spec-sheet/vite.config.ts"); err != nil {
		t.Skipf("projeto WSL de validação não está disponível: %v", err)
	}
	if !manager.usesVite(context.Background(), domain.Project{Path: "/home/silver/Sites/cj-spec-sheet"}) {
		t.Fatal("vite.config.ts deveria ser detectado no projeto WSL")
	}
}
