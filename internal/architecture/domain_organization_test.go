package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestDomainModelsStaySplitByAggregate keeps the first domain refactoring
// slice structural. The files intentionally share one package: this preserves
// the public domain import path while preventing a generic model file from
// becoming the ownership boundary again.
func TestDomainModelsStaySplitByAggregate(t *testing.T) {
	root := filepath.Join("..", "..", "internal", "domain")
	required := []string{
		"config.go",
		"project.go",
		"php.go",
		"runtime.go",
		"network.go",
		"security.go",
	}

	for _, name := range required {
		path := filepath.Join(root, name)
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ler modelo de agregado %s: %v", name, err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if err != nil {
			t.Fatalf("parsear modelo de agregado %s: %v", name, err)
		}
		if file.Name.Name != "domain" {
			t.Errorf("modelo de agregado %s está no package %s", name, file.Name.Name)
		}
		for _, importSpec := range file.Imports {
			importPath, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				t.Fatalf("ler import de %s: %v", name, err)
			}
			if strings.HasPrefix(importPath, "github.com/dougkusanagi/dev-lan/internal/") {
				t.Errorf("modelo de agregado %s depende de camada interna %s", name, importPath)
			}
		}
	}

	if _, err := os.Stat(filepath.Join(root, "model.go")); err == nil {
		t.Fatal("internal/domain/model.go voltou a concentrar os modelos")
	} else if !os.IsNotExist(err) {
		t.Fatalf("verificar remoção de model.go: %v", err)
	}
}
