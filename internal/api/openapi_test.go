package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAPIListsEveryHTTPContractOperation(t *testing.T) {
	var manifest struct {
		Operations map[string]struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		} `json:"operations"`
	}
	data, err := os.ReadFile("contract.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	openapi, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(openapi)
	for name, operation := range manifest.Operations {
		if operation.Method == "LOCAL" {
			continue
		}
		if !strings.Contains(text, "  "+operation.Path+":") {
			t.Errorf("operação %s ausente no OpenAPI: %s", name, operation.Path)
		}
	}
}
