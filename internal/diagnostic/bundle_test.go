package diagnostic

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreatesProtectedBundleWithManifest(t *testing.T) {
	target := filepath.Join(t.TempDir(), "support.zip")
	if err := Write(target, Manifest{OS: "test", Arch: "test"}, map[string][]byte{
		"config.json": []byte(`{"ok":true}`),
	}); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(target)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 2 {
		t.Fatalf("esperadas duas entradas, obtidas %d", len(archive.File))
	}
	for _, entry := range archive.File {
		if entry.Name == "manifest.json" {
			return
		}
	}
	t.Fatal("manifest.json ausente")
}

func TestWriteRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	err := Write(filepath.Join(dir, "support.zip"), Manifest{}, map[string][]byte{
		"../outside.txt": []byte("nope"),
	})
	if err == nil {
		t.Fatal("entrada fora do pacote deveria ser rejeitada")
	}
	if _, err := os.Stat(filepath.Join(dir, "outside.txt")); !os.IsNotExist(err) {
		t.Fatalf("não deveria haver arquivo fora do pacote: %v", err)
	}
}
