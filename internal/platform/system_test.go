package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortAvailabilityNeverTemporarilyBindsAllInterfaces(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("system.go"))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(data), "func IsPortAvailable")
	if start < 0 {
		t.Fatal("IsPortAvailable não encontrada")
	}
	body := string(data[start:])
	if strings.Contains(body, "0.0.0.0:") {
		t.Fatal("a sondagem de porta não pode escutar em todas as interfaces")
	}
}
