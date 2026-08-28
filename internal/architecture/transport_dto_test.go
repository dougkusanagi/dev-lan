package architecture

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestTransportBoundariesDoNotUseDynamicPayloadTypes(t *testing.T) {
	root := filepath.Join("..", "..")
	paths := []string{
		filepath.Join(root, "internal", "api"),
		filepath.Join(root, "cmd", "devlan"),
		filepath.Join(root, "internal", "config", "export.go"),
		filepath.Join(root, "internal", "platform", "hyperv_firewall.go"),
		filepath.Join(root, "internal", "app", "diagnostics.go"),
	}
	forbidden := regexp.MustCompile(`map\[string\]any|\[\]any|\.\.\.any|\bvalue any\b`)

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("verificar fronteira %s: %v", path, err)
		}
		check := func(filePath string) {
			if filepath.Ext(filePath) != ".go" || strings.HasSuffix(filepath.Base(filePath), "_test.go") {
				return
			}
			source, readErr := os.ReadFile(filePath)
			if readErr != nil {
				t.Fatalf("ler fronteira %s: %v", filePath, readErr)
			}
			if match := forbidden.Find(source); match != nil {
				t.Errorf("fronteira %s contém tipo de payload dinâmico %q", filePath, match)
			}
		}
		if info.IsDir() {
			err = filepath.Walk(path, func(filePath string, _ os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				check(filePath)
				return nil
			})
			if err != nil {
				t.Fatalf("percorrer fronteira %s: %v", path, err)
			}
		} else {
			check(path)
		}
	}
}
