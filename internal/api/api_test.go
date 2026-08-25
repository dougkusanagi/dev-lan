package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/app"
)

func TestAuthenticatedLoopbackAPI(t *testing.T) {
	service := app.New(t.TempDir())
	server := New(service)
	endpoint, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	if _, err := New(service).Start(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("segunda API deveria ser rejeitada: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/health", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("token ausente deveria retornar 401, obtido %d", recorder.Code)
	}

	tokenData, err := os.ReadFile(endpoint.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/health", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tokenData)))
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("health autenticado inválido: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAPIRejectsNonLoopback(t *testing.T) {
	service := app.New(t.TempDir())
	server := New(service)
	endpoint, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	tokenData, err := os.ReadFile(endpoint.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/v1/health", nil)
	request.RemoteAddr = "192.168.1.10:1234"
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tokenData)))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("origem não-local deveria retornar 403, obtido %d", recorder.Code)
	}
}

func TestWSLCommandProtocolIsAuthenticatedAndAllowListed(t *testing.T) {
	service := app.New(t.TempDir())
	server := New(service)
	endpoint, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	tokenData, err := os.ReadFile(endpoint.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"command": "status", "args": []string{}})
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/command", bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tokenData)))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"project_count"`) {
		t.Fatalf("comando status inválido: %d %s", recorder.Code, recorder.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"command": "exec", "args": []string{"whoami"}})
	request = httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/command", bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tokenData)))
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("comando não allow-listed deveria retornar 404, obteve %d", recorder.Code)
	}
}

func TestSameTokenPathAcceptsMountedWindowsPathOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("conversão de caminho montado só é aplicável no Linux")
	}
	if !sameTokenPath(`C:\Users\dev\AppData\Local\DevLAN\api.token`, `/mnt/c/Users/dev/AppData/Local/DevLAN/api.token`) {
		t.Fatal("caminho Windows equivalente no mount WSL foi rejeitado")
	}
}
