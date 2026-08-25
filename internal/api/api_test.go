package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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
