package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/app"
)

func TestLoopbackAPIHealthAndStatus(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	service := app.New(t.TempDir())
	server := New(service)
	endpoint, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	if server.httpServer.ReadHeaderTimeout <= 0 || server.httpServer.ReadTimeout <= server.httpServer.ReadHeaderTimeout || server.httpServer.MaxHeaderBytes > 1<<20 {
		t.Fatalf("limites HTTP inseguros: %#v", server.httpServer)
	}
	if _, err := New(service).Start(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("segunda API deveria ser rejeitada: %v", err)
	}

	// Safe GET on loopback succeeds for browser/SPA
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/health", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("health inválido: %d %s", recorder.Code, recorder.Body.String())
	}

	// Status endpoint returns valid JSON with protocol version and without secrets
	request = httptest.NewRequest(http.MethodGet, "http://devlan.localhost/api/v1/status", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"protocolVersion":1`) {
		t.Fatalf("status inválido: %d %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), endpoint.TokenFile) {
		t.Fatal("status vazou caminho ou segredo do token")
	}
}

func TestLoopbackAPIBindsIPv6ForConfiguredPort(t *testing.T) {
	service := app.New(t.TempDir())
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	uiPort := reserved.Addr().(*net.TCPAddr).Port
	_ = reserved.Close()
	cfg, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.UIPort = uiPort
	if err := service.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	server := New(service)
	endpoint, err := server.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	if len(server.listeners) != 2 {
		t.Fatalf("servidor deveria manter listeners IPv4 e IPv6, obtido %d", len(server.listeners))
	}
	_, endpointPort, err := net.SplitHostPort(endpoint.Address)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://[::1]:"+endpointPort+"/api/v1/health", nil)
	request.RemoteAddr = "[::1]:1234"
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("handler deveria aceitar loopback IPv6, obtido %d", recorder.Code)
	}
}

func TestHostAndOriginAllowlist(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	service := app.New(t.TempDir())
	server := New(service)

	// Disallowed Host header (e.g. DNS rebinding)
	request := httptest.NewRequest(http.MethodGet, "http://attacker.com/api/v1/health", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Host = "attacker.com"
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("host não permitido deveria retornar 403, obtido %d", recorder.Code)
	}

	// Allowed devlan.localhost host
	request = httptest.NewRequest(http.MethodGet, "https://devlan.localhost/api/v1/health", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Host = "devlan.localhost"
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("devlan.localhost deveria ser aceito, obtido %d", recorder.Code)
	}

	// Disallowed Origin header
	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/health", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Origin", "http://evil-site.com")
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("origem não permitida deveria retornar 403, obtido %d", recorder.Code)
	}

	// Allowed Origin header
	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/health", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Origin", "https://devlan.localhost")
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("origem devlan.localhost deveria ser aceita, obtido %d", recorder.Code)
	}

	// Cookies are not isolated by port: another loopback web server must not
	// become a trusted CSRF origin merely because it uses the same hostname.
	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/health", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Origin", "http://127.0.0.1:9999")
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("origem loopback em porta errada deveria retornar 403, obtido %d", recorder.Code)
	}
}

func TestCSRFAndBearerProtectionOnMutations(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
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
	token := strings.TrimSpace(string(tokenData))

	// 1. Mutation without token or CSRF is rejected with 401
	body := bytes.NewReader([]byte(`{}`))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/reload", body)
	request.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("mutação sem auth/CSRF deveria retornar 401, obtido %d", recorder.Code)
	}

	// 2. Mutation with Bearer token succeeds
	body = bytes.NewReader([]byte(`{}`))
	request = httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/reload", body)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer "+token)
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("mutação com Bearer token deveria retornar 200, obtido %d %s", recorder.Code, recorder.Body.String())
	}

	// 3. Mutation with CSRF cookie and matching header succeeds
	csrfToken := "test-csrf-secret-1234"
	body = bytes.NewReader([]byte(`{}`))
	request = httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/reload", body)
	request.RemoteAddr = "127.0.0.1:1234"
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	request.Header.Set(csrfHeaderName, csrfToken)
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("mutação com CSRF válido deveria retornar 200, obtido %d %s", recorder.Code, recorder.Body.String())
	}

	// 4. Mutation with mismatched CSRF token is rejected
	body = bytes.NewReader([]byte(`{}`))
	request = httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/reload", body)
	request.RemoteAddr = "127.0.0.1:1234"
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	request.Header.Set(csrfHeaderName, "wrong-csrf-token")
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("mutação com CSRF divergente deveria retornar 401, obtido %d", recorder.Code)
	}
}

func TestSPAHistoryFallbackAndSecurityHeaders(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	service := app.New(t.TempDir())
	server := New(service)

	// GET / returns index.html, security headers, and sets CSRF cookie
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET / deveria retornar 200, obtido %d", recorder.Code)
	}
	if recorder.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options ausente ou incorreto: %s", recorder.Header().Get("X-Frame-Options"))
	}
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options ausente: %s", recorder.Header().Get("X-Content-Type-Options"))
	}
	if !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Errorf("CSP ausente: %s", recorder.Header().Get("Content-Security-Policy"))
	}
	if !strings.Contains(recorder.Header().Get("Set-Cookie"), csrfCookieName) {
		t.Errorf("Cookie CSRF não foi gerado no GET /: %s", recorder.Header().Get("Set-Cookie"))
	}

	// Non-API route /projects/sample-app falls back to index.html (history fallback)
	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/projects/sample-app", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("history fallback deveria servir index.html com 200, obtido %d", recorder.Code)
	}

	// Unknown API route /api/v1/unknown returns JSON 404, never HTML!
	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/unknown", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("rota de API desconhecida deveria retornar JSON 404, obtido %d %s", recorder.Code, recorder.Body.String())
	}

	// Missing static files must not be rewritten to index.html.
	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/assets/missing.js", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || strings.Contains(recorder.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("asset ausente deveria retornar 404, sem history fallback: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAPIRejectsNonLoopback(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
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
	t.Setenv("DEVLAN_TEST_MOCK", "1")
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
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"totalProjects"`) {
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
