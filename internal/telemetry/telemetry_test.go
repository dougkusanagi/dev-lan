package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestTelemetryIsOptInAndSanitized(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Record("reload", map[string]string{"path": `/home/user/project`, "host": "192.168.1.40", "mode": "php"}); err != nil {
		t.Fatal(err)
	}
	if size, _ := store.QueueSize(); size != 0 {
		t.Fatalf("telemetria deveria estar desabilitada por padrão: %d", size)
	}
	if err := store.SetConsent(true, "https://telemetry.example.test/events"); err != nil {
		t.Fatal(err)
	}
	if err := store.Record("reload", map[string]string{"path": `/home/user/project`, "host": "192.168.1.40", "mode": "php"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.queuePath())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || string(data) == "path" {
		t.Fatalf("fila de telemetria vazia: %s", data)
	}
	if string(data) != "" && (contains(string(data), "/home") || contains(string(data), "192.168.1.40")) {
		t.Fatalf("caminho/endereço não deveriam ser enviados: %s", data)
	}
}

func TestTelemetrySendRequiresConsentAndClearsQueue(t *testing.T) {
	var received []Event
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	store := NewStore(t.TempDir())
	if err := store.SetConsent(true, server.URL); err != nil {
		t.Fatal(err)
	}
	if err := store.Record("install", map[string]string{"result": "ok"}); err != nil {
		t.Fatal(err)
	}
	count, err := store.Send(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || len(received) != 1 || received[0].Name != "install" {
		t.Fatalf("telemetria enviada incorretamente: %d %#v", count, received)
	}
	if count, _ := store.QueueSize(); count != 0 {
		t.Fatalf("fila deveria estar vazia após envio: %d", count)
	}
}

func contains(value, needle string) bool {
	for index := 0; index+len(needle) <= len(value); index++ {
		if value[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
