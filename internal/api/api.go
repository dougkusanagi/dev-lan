// Package api exposes the loopback web server and authenticated API
// shared by the browser GUI, Windows service, CLI and WSL clients.
package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dougkusanagi/dev-lan/frontend"
	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

const (
	ProtocolVersion = 1
	tokenSize       = 32
	csrfCookieName  = "devlan_csrf"
	csrfHeaderName  = "X-DevLAN-CSRF-Token"
	maxRequestBody  = 1 << 20
)

var (
	ErrInvalidEndpoint = errors.New("endpoint local do DevLAN inválido")
	ErrAlreadyRunning  = errors.New("API local do DevLAN já está em execução")
)

type Endpoint struct {
	Version   int    `json:"version"`
	Address   string `json:"address"`
	TokenFile string `json:"token_file"`
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
}

type Server struct {
	service *app.App

	mu         sync.Mutex
	listener   net.Listener
	listeners  []net.Listener
	httpServer *http.Server
	endpoint   Endpoint
}

func New(service *app.App) *Server {
	return &Server{service: service}
}

func (s *Server) Start() (Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.endpoint, nil
	}
	if err := s.service.EnsureState(); err != nil {
		return Endpoint{}, err
	}
	cfg, err := s.service.Config()
	if err != nil {
		return Endpoint{}, fmt.Errorf("validar configuração antes de iniciar a API: %w", err)
	}
	uiPort := cfg.UIPort
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		uiPort = 0
	} else if uiPort == 0 {
		uiPort = 3210
	}
	expectedPort := strconv.Itoa(uiPort)

	files := s.service.APIEndpointFiles()
	if _, err := os.Stat(files.Endpoint); err == nil {
		if endpoint, readErr := ReadEndpoint(files.Endpoint, files.Token); readErr == nil {
			_, port, _ := net.SplitHostPort(endpoint.Address)
			if (expectedPort == "0" && port != "") || port == expectedPort {
				checkContext, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				response, requestErr := NewClientFromFiles(files).Do(checkContext, http.MethodGet, "/v1/health", nil)
				cancel()
				if response != nil {
					_ = response.Body.Close()
				}
				if requestErr == nil && response != nil && response.StatusCode < 400 {
					return endpoint, ErrAlreadyRunning
				}
			}
		}
		_ = os.Remove(files.Endpoint)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Endpoint{}, err
	}
	token, err := ensureToken(files.Token)
	if err != nil {
		return Endpoint{}, err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(uiPort))
	if err != nil {
		return Endpoint{}, fmt.Errorf("iniciar API local na ui_port %d: %w", uiPort, err)
	}
	listeners := []net.Listener{listener}
	// The mock uses port 0, so a second listener would receive a different
	// ephemeral port. Production always uses the configured stable ui_port.
	if os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		listener6, listen6Err := net.Listen("tcp", "[::1]:"+strconv.Itoa(uiPort))
		if listen6Err != nil {
			_ = listener.Close()
			return Endpoint{}, fmt.Errorf("iniciar API local IPv6 na ui_port %d: %w", uiPort, listen6Err)
		}
		listeners = append(listeners, listener6)
	}

	endpoint := Endpoint{
		Version:   ProtocolVersion,
		Address:   listener.Addr().String(),
		TokenFile: files.Token,
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}

	server := &http.Server{
		Handler:           s.Handler(token),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	if err := writeEndpoint(files.Endpoint, endpoint); err != nil {
		for _, item := range listeners {
			_ = item.Close()
		}
		return Endpoint{}, err
	}

	s.listener = listener
	s.listeners = listeners
	s.httpServer = server
	s.endpoint = endpoint

	for _, item := range listeners {
		go func(current net.Listener) {
			if err := server.Serve(current); err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.service.Audit("API_STOP", "servidor local encerrado: "+err.Error())
			}
		}(item)
	}

	s.service.Audit("API_START", "API local autenticada em "+endpoint.Address)
	return endpoint, nil
}

func (s *Server) Serve(ctx context.Context) error {
	if _, err := s.Start(); err != nil {
		return err
	}
	<-ctx.Done()
	return s.Close(context.Background())
}

func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	server := s.httpServer
	listeners := s.listeners
	endpointPath := s.service.APIEndpointFiles().Endpoint
	s.listener = nil
	s.listeners = nil
	s.httpServer = nil
	s.endpoint = Endpoint{}
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	err := server.Shutdown(ctx)
	for _, listener := range listeners {
		_ = listener.Close()
	}
	_ = os.Remove(endpointPath)
	s.service.Audit("API_STOP", "API local encerrada")
	return err
}

func (s *Server) Handler(token ...string) http.Handler {
	secret := ""
	if len(token) > 0 {
		secret = token[0]
	} else {
		loaded, err := readToken(s.service.APIEndpointFiles().Token)
		if err == nil {
			secret = loaded
		}
	}

	apiMux := http.NewServeMux()

	// Health & Status
	apiMux.HandleFunc("/api/v1/health", s.handleHealth)
	apiMux.HandleFunc("/v1/health", s.handleHealth)
	apiMux.HandleFunc("/api/v1/status", s.handleStatus)
	apiMux.HandleFunc("/v1/status", s.handleStatus)
	apiMux.HandleFunc("/api/v1/topology", s.handleTopology)
	apiMux.HandleFunc("/v1/topology", s.handleTopology)
	apiMux.HandleFunc("/api/v1/overview", s.handleOverview)
	apiMux.HandleFunc("/v1/overview", s.handleOverview)
	apiMux.HandleFunc("/api/v1/operations/", s.handleOperation)
	apiMux.HandleFunc("/v1/operations/", s.handleOperation)
	apiMux.HandleFunc("/api/v1/events", s.handleEvents)
	apiMux.HandleFunc("/v1/events", s.handleEvents)

	// Projects
	apiMux.HandleFunc("/api/v1/projects", s.handleProjects)
	apiMux.HandleFunc("/v1/projects", s.handleProjects)
	apiMux.HandleFunc("/api/v1/projects/logs", s.handleProjectLogs)
	apiMux.HandleFunc("/api/v1/projects/link", s.handleProjectLink)
	apiMux.HandleFunc("/api/v1/projects/unlink", s.handleProjectUnlink)
	apiMux.HandleFunc("/api/v1/projects/hide", s.handleProjectHide)
	apiMux.HandleFunc("/api/v1/projects/unhide", s.handleProjectUnhide)
	apiMux.HandleFunc("/api/v1/projects/config", s.handleProjectConfig)
	apiMux.HandleFunc("/api/v1/projects/start", s.handleProjectStart)
	apiMux.HandleFunc("/api/v1/projects/stop", s.handleProjectStop)
	apiMux.HandleFunc("/api/v1/projects/restart", s.handleProjectRestart)
	apiMux.HandleFunc("/api/v1/projects/build", s.handleProjectBuild)
	apiMux.HandleFunc("/api/v1/projects/deps", s.handleProjectDeps)
	apiMux.HandleFunc("/api/v1/projects/tls", s.handleProjectTLS)

	// Parks
	apiMux.HandleFunc("/api/v1/parks/park", s.handlePark)
	apiMux.HandleFunc("/api/v1/parks/unpark", s.handleUnpark)

	// Config
	apiMux.HandleFunc("/api/v1/config", s.handleConfig)
	apiMux.HandleFunc("/v1/config", s.handleConfig)
	apiMux.HandleFunc("/api/v1/config/export", s.handleConfigExport)
	apiMux.HandleFunc("/api/v1/config/import", s.handleConfigImport)
	apiMux.HandleFunc("/api/v1/reload", s.handleReload)
	apiMux.HandleFunc("/v1/reload", s.handleReload)

	// PHP
	apiMux.HandleFunc("/api/v1/php/versions", s.handlePHPVersions)
	apiMux.HandleFunc("/api/v1/php/install", s.handlePHPInstall)
	apiMux.HandleFunc("/api/v1/php/remove", s.handlePHPRemove)
	apiMux.HandleFunc("/api/v1/php/default", s.handlePHPDefault)

	// Metrics & Doctor
	apiMux.HandleFunc("/api/v1/metrics", s.handleMetrics)
	apiMux.HandleFunc("/api/v1/doctor", s.handleDoctor)
	apiMux.HandleFunc("/api/v1/doctor/fix", s.handleDoctorFix)

	// Security
	apiMux.HandleFunc("/api/v1/security/audit", s.handleSecurityAudit)
	apiMux.HandleFunc("/api/v1/security/trust", s.handleSecurityTrust)

	// WSL Command protocol
	apiMux.HandleFunc("/api/v1/command", s.handleCommand)
	apiMux.HandleFunc("/v1/command", s.handleCommand)

	// Global 404 for unhandled API routes
	apiMux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		writeJSONError(writer, http.StatusNotFound, "rota da API local não encontrada")
	})

	var distFS fs.FS
	if sub, err := fs.Sub(frontend.Assets, "dist"); err == nil {
		distFS = sub
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// 1. Loopback restriction (ui_access = local)
		if !isLoopback(request.RemoteAddr) {
			writeJSONError(writer, http.StatusForbidden, "API local aceita somente loopback")
			return
		}

		// 2. Host allowlist validation
		if !isValidHost(request.Host) {
			writeJSONError(writer, http.StatusForbidden, "Host não permitido: "+request.Host)
			return
		}

		// 3. Origin validation for browser requests
		if origin := request.Header.Get("Origin"); origin != "" {
			if !s.isValidOrigin(origin) {
				writeJSONError(writer, http.StatusForbidden, "Origem não permitida: "+origin)
				return
			}
		}

		// 4. Security headers on all responses
		setSecurityHeaders(writer)

		// 5. Route to API or SPA
		reqPath := path.Clean(request.URL.Path)
		if strings.HasPrefix(reqPath, "/api/") || strings.HasPrefix(reqPath, "/v1/") {
			// Limit body size for API
			if request.Body != nil {
				request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
			}
			// Authentication & CSRF check on mutations
			if isMutation(request.Method) {
				if !isAuthorizedOrCSRF(request, secret) {
					writeJSONError(writer, http.StatusUnauthorized, "token da API local ou CSRF ausente ou inválido")
					return
				}
			}
			apiMux.ServeHTTP(writer, request)
			return
		}

		// Serve embedded SPA static assets with history fallback
		s.serveSPA(writer, request, distFS)
	})
}

func (s *Server) serveSPA(writer http.ResponseWriter, request *http.Request, distFS fs.FS) {
	if distFS == nil {
		ensureCSRFCookie(writer, request)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("<!DOCTYPE html><html><body><h1>DevLAN</h1><p>Frontend não compilado.</p></body></html>"))
		return
	}

	reqPath := path.Clean(strings.TrimPrefix(request.URL.Path, "/"))
	if reqPath == "" || reqPath == "." {
		reqPath = "index.html"
	}

	// Try opening static assets. A missing asset must remain a 404; serving the
	// SPA document here makes browsers report misleading JS/CSS parse errors and
	// violates the API/static boundary.
	if strings.HasPrefix(reqPath, "assets/") || (reqPath != "index.html" && filepath.Ext(reqPath) != "" && filepath.Ext(reqPath) != ".html") {
		file, err := distFS.Open(reqPath)
		if err == nil {
			defer file.Close()
			stat, statErr := file.Stat()
			if statErr == nil && !stat.IsDir() {
				if strings.HasPrefix(reqPath, "assets/") {
					writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					writer.Header().Set("Cache-Control", "no-cache")
				}
				http.FileServer(http.FS(distFS)).ServeHTTP(writer, request)
				return
			}
		}
		http.NotFound(writer, request)
		return
	}

	// History fallback -> index.html
	indexFile, err := distFS.Open("index.html")
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer indexFile.Close()

	// Ensure CSRF cookie exists for SPA mutations
	ensureCSRFCookie(writer, request)

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	_, _ = io.Copy(writer, indexFile)
}

func ensureCSRFCookie(writer http.ResponseWriter, request *http.Request) {
	if _, err := request.Cookie(csrfCookieName); err != nil {
		tokenBytes := make([]byte, 16)
		_, _ = rand.Read(tokenBytes)
		token := hex.EncodeToString(tokenBytes)
		http.SetCookie(writer, &http.Cookie{
			Name:     csrfCookieName,
			Value:    token,
			Path:     "/",
			SameSite: http.SameSiteLaxMode,
			HttpOnly: false, // Accessible by JS client for CSRF header
		})
	}
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

func isValidHost(hostHeader string) bool {
	host, _, err := net.SplitHostPort(hostHeader)
	if err != nil {
		host = hostHeader
	}
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost" || host == "devlan.localhost"
}

func (s *Server) isValidOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if !strings.EqualFold(u.Scheme, "https") && !strings.EqualFold(u.Scheme, "http") {
		return false
	}

	// Origin is a full browser security principal: host alone is insufficient.
	// Cookies are not scoped by port, so accepting any loopback port would let a
	// malicious local web server set the double-submit CSRF cookie and mutate
	// this API. Only the actual direct UI origin and the fixed Caddy origin are
	// valid.
	if strings.EqualFold(u.Scheme, "https") && strings.EqualFold(u.Host, "devlan.localhost") {
		return true
	}
	if !strings.EqualFold(u.Scheme, "http") {
		return false
	}
	cfg, err := s.service.Config()
	if err != nil {
		return false
	}
	uiPort := cfg.UIPort
	if uiPort == 0 {
		uiPort = 3210
	}
	if u.Port() != strconv.Itoa(uiPort) {
		return false
	}
	host := strings.Trim(strings.ToLower(strings.TrimSpace(u.Hostname())), "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

func setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self' data:;")
}

func isMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	default:
		return false
	}
}

func isAuthorizedOrCSRF(request *http.Request, secret string) bool {
	// 1. Check Bearer token (CLI / WSL)
	if authorized(request, secret) {
		return true
	}

	// 2. Check CSRF header vs cookie (Browser)
	headerToken := strings.TrimSpace(request.Header.Get(csrfHeaderName))
	cookie, err := request.Cookie(csrfCookieName)
	if err == nil && headerToken != "" && cookie.Value != "" {
		if subtle.ConstantTimeCompare([]byte(headerToken), []byte(cookie.Value)) == 1 {
			return true
		}
	}

	return false
}

func authorized(request *http.Request, token string) bool {
	if token == "" {
		return false
	}
	value := strings.TrimSpace(request.Header.Get("Authorization"))
	if len(value) < len("Bearer ") || !strings.EqualFold(value[:len("Bearer ")], "Bearer ") {
		return false
	}
	provided := strings.TrimSpace(value[len("Bearer "):])
	if len(provided) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

// Handlers

func (s *Server) handleHealth(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": ProtocolVersion,
		"runtime": runtime.GOOS + "/" + runtime.GOARCH,
	})
}

func (s *Server) handleStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	view, err := BuildStatusView(request.Context(), s.service)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (s *Server) handleTopology(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	writeJSON(writer, http.StatusOK, BuildTopologyView(request.Context(), s.service))
}

func (s *Server) handleOverview(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	filter := request.URL.Query().Get("filter")
	view, err := BuildOverviewView(request.Context(), s.service, filter)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (s *Server) handleOperation(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	id := strings.TrimPrefix(path.Clean(request.URL.Path), "/api/v1/operations/")
	if strings.HasPrefix(request.URL.Path, "/v1/") {
		id = strings.TrimPrefix(path.Clean(request.URL.Path), "/v1/operations/")
	}
	if id == "" || id == "." {
		writeJSONError(writer, http.StatusBadRequest, "operationId ausente")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	result, ok := s.operationResponse(ctx, id)
	if !ok {
		writeJSONError(writer, http.StatusNotFound, "operação não encontrada")
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (s *Server) handleEvents(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeJSONError(writer, http.StatusInternalServerError, "SSE não suportado pelo servidor")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache, no-store")
	writer.Header().Set("Connection", "keep-alive")
	_, _ = writer.Write([]byte(": devlan events\n\n"))
	flusher.Flush()
	updates, stop := s.service.SubscribeOperations(request.Context())
	defer stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case state, open := <-updates:
			if !open {
				return
			}
			result := operationResult(request.Context(), s.service, state)
			eventName := "operation-progress"
			if isTerminalPhase(state.Phase) && state.ProjectName != "" {
				eventName = "project-state-changed"
			}
			data, err := json.Marshal(result)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", eventName, data)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = writer.Write([]byte(": heartbeat\n\n"))
			flusher.Flush()
		}
	}
}

func (s *Server) handleProjects(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	filter := request.URL.Query().Get("filter")
	views, err := BuildProjectViews(request.Context(), s.service, filter)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, views)
}

func (s *Server) handleProjectLogs(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	name := request.URL.Query().Get("name")
	linesStr := request.URL.Query().Get("lines")
	lines := 100
	if l, err := strconv.Atoi(linesStr); err == nil && l > 0 {
		lines = l
	}
	devLogs, err := s.service.ProjectDevLogs(request.Context(), name, lines)
	if err == nil && strings.TrimSpace(devLogs) != "" {
		writeJSON(writer, http.StatusOK, map[string]string{"logs": devLogs})
		return
	}
	globalLogs, _ := s.service.Logs("devlan")
	writeJSON(writer, http.StatusOK, map[string]string{
		"logs": fmt.Sprintf("Nenhum log de servidor dev para %s.\n\nLogs do DevLAN:\n%s", name, globalLogs),
	})
}

func (s *Server) handleProjectLink(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	proj, res, err := s.service.Link(request.Context(), input.Name, input.Path)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"message":  fmt.Sprintf("Projeto %s vinculado.", proj.Name),
		"warnings": res.Warnings,
	})
	InvalidateReadModelCache(s.service)
}

func (s *Server) handleProjectUnlink(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	proj, res, err := s.service.Unlink(request.Context(), input.Name)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"message":  fmt.Sprintf("Projeto %s desvinculado.", proj.Name),
		"warnings": res.Warnings,
	})
	InvalidateReadModelCache(s.service)
}

func (s *Server) handleProjectHide(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	res, err := s.service.IgnoreProject(request.Context(), input.Name)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"message":  fmt.Sprintf("Projeto %s ocultado.", input.Name),
		"warnings": res.Warnings,
	})
	InvalidateReadModelCache(s.service)
}

func (s *Server) handleProjectUnhide(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	res, err := s.service.UnignoreProject(request.Context(), input.Path)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"message":  fmt.Sprintf("Projeto %s restaurado.", input.Path),
		"warnings": res.Warnings,
	})
	InvalidateReadModelCache(s.service)
}

func (s *Server) handleProjectConfig(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var update ProjectConfigUpdate
	if err := json.NewDecoder(request.Body).Decode(&update); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "configuração de projeto inválida")
		return
	}
	ctx := request.Context()
	if update.TLSEnabled != nil && update.Mode == "" && update.PHPVersion == "" && update.PHPPreset == "" &&
		update.RoutePort == nil && !update.RoutePortAuto && update.StaticDir == "" && update.DevCommand == "" && update.DevPort == 0 {
		target := *update.TLSEnabled
		s.startAsyncOperation(writer, request, "tls", update.Name, update.OperationID, 90*time.Second,
			func(workCtx context.Context) (uint64, []string, error) {
				result, _, err := s.service.SetProjectTLS(workCtx, update.Name, target)
				return result.Revision, result.Warnings, err
			})
		return
	}
	if update.Mode != "" {
		m, err := domain.ParseMode(update.Mode)
		if err != nil {
			writeJSONError(writer, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := s.service.SetProjectMode(ctx, update.Name, &m); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	if update.TLSEnabled != nil {
		if _, _, err := s.service.SetProjectTLS(ctx, update.Name, *update.TLSEnabled); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	if update.PHPVersion != "" {
		if _, err := s.service.SetProjectPHPVersion(ctx, update.Name, update.PHPVersion); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	if update.PHPPreset != "" {
		if _, err := s.service.SetProjectPHPPreset(ctx, update.Name, update.PHPPreset); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	if update.RoutePortAuto || update.RoutePort != nil {
		var port *int
		if !update.RoutePortAuto {
			port = update.RoutePort
		}
		result, err := s.service.SetRoutePort(ctx, update.Name, port)
		if err != nil {
			writeApplyError(writer, http.StatusConflict, result, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{
			"message":  "Configuração do projeto salva.",
			"status":   result.Status,
			"revision": result.Revision,
			"warnings": result.Warnings,
		})
		InvalidateReadModelCache(s.service)
		return
	}
	if update.StaticDir != "" {
		if _, err := s.service.SetProjectStaticDir(ctx, update.Name, update.StaticDir); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	if update.DevCommand != "" {
		if _, err := s.service.SetProjectDevCommand(ctx, update.Name, update.DevCommand); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	if update.DevPort > 0 {
		if _, err := s.service.SetProjectDevPort(ctx, update.Name, update.DevPort); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	InvalidateReadModelCache(s.service)
	writeJSON(writer, http.StatusOK, map[string]string{"message": "Configuração do projeto salva."})
}

func writeApplyError(writer http.ResponseWriter, status int, result app.ApplyResult, err error) {
	writeJSON(writer, status, map[string]any{
		"error":    err.Error(),
		"status":   result.Status,
		"revision": result.Revision,
		"warnings": result.Warnings,
	})
}

func (s *Server) handleProjectStart(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name        string `json:"name"`
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	s.startAsyncOperation(writer, request, "start", input.Name, input.OperationID, 90*time.Second,
		func(ctx context.Context) (uint64, []string, error) {
			err := s.service.StartDev(ctx, input.Name)
			return currentRevision(s.service), nil, err
		})
}

func (s *Server) handleProjectStop(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name        string `json:"name"`
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	s.startAsyncOperation(writer, request, "stop", input.Name, input.OperationID, 45*time.Second,
		func(ctx context.Context) (uint64, []string, error) {
			err := s.service.StopDev(ctx, input.Name)
			return currentRevision(s.service), nil, err
		})
}

func (s *Server) handleProjectRestart(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name        string `json:"name"`
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	s.startAsyncOperation(writer, request, "restart", input.Name, input.OperationID, 90*time.Second,
		func(ctx context.Context) (uint64, []string, error) {
			err := s.service.RestartDev(ctx, input.Name)
			return currentRevision(s.service), nil, err
		})
}

func (s *Server) handleProjectBuild(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	out, err := s.service.BuildProject(request.Context(), input.Name)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"output": out})
}

func (s *Server) handleProjectDeps(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	out, err := s.service.InstallDeps(request.Context(), input.Name)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"output": out})
}

func (s *Server) handleProjectTLS(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name        string `json:"name"`
		Enabled     bool   `json:"enabled"`
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	s.startAsyncOperation(writer, request, "tls", input.Name, input.OperationID, 90*time.Second,
		func(ctx context.Context) (uint64, []string, error) {
			result, _, err := s.service.SetProjectTLS(ctx, input.Name, input.Enabled)
			return result.Revision, result.Warnings, err
		})
}

func (s *Server) handlePark(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	park, res, err := s.service.Park(request.Context(), input.Path)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"message":  fmt.Sprintf("Pasta %s estacionada.", park.Path),
		"warnings": res.Warnings,
	})
	InvalidateReadModelCache(s.service)
}

func (s *Server) handleUnpark(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	park, res, err := s.service.Unpark(request.Context(), input.Path)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"message":  fmt.Sprintf("Pasta %s desestacionada.", park.Path),
		"warnings": res.Warnings,
	})
	InvalidateReadModelCache(s.service)
}

func (s *Server) handleConfig(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		view, err := BuildGlobalConfigView(s.service)
		if err != nil {
			writeJSONError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, view)
		return
	}
	if request.Method == http.MethodPost {
		var view GlobalConfigView
		if err := json.NewDecoder(request.Body).Decode(&view); err != nil {
			writeJSONError(writer, http.StatusBadRequest, "configuração global inválida")
			return
		}
		if view.DefaultMode != "" {
			if _, err := domain.ParseMode(view.DefaultMode); err != nil {
				writeJSONError(writer, http.StatusBadRequest, err.Error())
				return
			}
		}
		settings := app.GlobalSettings{
			DefaultMode:       view.DefaultMode,
			WindowsPort:       view.WindowsPort,
			HTTPSPort:         view.HTTPSPort,
			TLSEnabled:        view.TLSEnabled,
			PHPDefaultVersion: view.PHPDefaultVersion,
			Allowlist:         view.Allowlist,
		}
		if _, err := s.service.SaveGlobalSettings(request.Context(), settings); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		InvalidateReadModelCache(s.service)
		writeJSON(writer, http.StatusOK, map[string]string{"message": "Configuração global salva."})
		return
	}
	methodNotAllowed(writer, http.MethodGet+", "+http.MethodPost)
}

func (s *Server) handleConfigExport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	data, err := s.service.ExportConfig()
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(data)
}

func (s *Server) handleConfigImport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	data, err := io.ReadAll(request.Body)
	if err != nil {
		writeJSONError(writer, http.StatusBadRequest, "ler corpo da requisição: "+err.Error())
		return
	}
	res, err := s.service.ImportConfig(request.Context(), data)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"message":  "Configuração importada e aplicada com sucesso.",
		"warnings": res.Warnings,
	})
}

func (s *Server) handleReload(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	result, err := s.service.Reload(request.Context())
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	InvalidateReadModelCache(s.service)
	writeJSON(writer, http.StatusOK, map[string]any{
		"message": "Configurações recarregadas com sucesso.",
		"result":  result,
	})
}

func (s *Server) handlePHPVersions(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	items, err := BuildPHPVersionsView(request.Context(), s.service)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, items)
}

func (s *Server) handlePHPInstall(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Version    string   `json:"version"`
		Extensions []string `json:"extensions"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	res, err := s.service.PHPInstall(request.Context(), input.Version, input.Extensions)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"message":  fmt.Sprintf("PHP %s instalado.", input.Version),
		"warnings": res.Warnings,
	})
	InvalidateColdReadModelCache(s.service)
}

func (s *Server) handlePHPRemove(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	res, err := s.service.PHPRemove(request.Context(), input.Version)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"message":  fmt.Sprintf("PHP %s removido.", input.Version),
		"warnings": res.Warnings,
	})
	InvalidateColdReadModelCache(s.service)
}

func (s *Server) handlePHPDefault(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	res, err := s.service.SetDefaultPHPVersion(request.Context(), input.Version)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"message":  fmt.Sprintf("PHP padrão alterado para %s.", input.Version),
		"warnings": res.Warnings,
	})
	InvalidateColdReadModelCache(s.service)
}

func (s *Server) handleMetrics(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	project := request.URL.Query().Get("project")
	rawRange := request.URL.Query().Get("range")
	if rawRange == "" {
		rawRange = "1h"
	}
	snapshot, err := BuildMetricsSnapshot(s.service, project, rawRange)
	if err != nil {
		writeJSONError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, snapshot)
}

func (s *Server) handleDoctor(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	project := request.URL.Query().Get("project")
	checks, err := BuildDoctorChecksView(request.Context(), s.service, project)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, checks)
}

func (s *Server) handleDoctorFix(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Action string `json:"action"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	ctx := request.Context()
	switch input.Action {
	case "reload":
		_, err := s.service.Reload(ctx)
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	case "firewall":
		if err := s.service.ReconcileFirewall(ctx); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	case "topology", "topology-repair":
		if _, err := s.service.RepairM8(ctx); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	case "trust":
		if err := s.service.Trust(ctx); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	case "restart-dev":
		if input.Target != "" {
			if err := s.service.RestartDev(ctx, input.Target); err != nil {
				writeJSONError(writer, http.StatusConflict, err.Error())
				return
			}
		}
	default:
		_, _ = s.service.Reload(ctx)
	}
	InvalidateReadModelCache(s.service)
	writeJSON(writer, http.StatusOK, map[string]string{"message": "Correção aplicada."})
}

func (s *Server) handleSecurityAudit(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	linesStr := request.URL.Query().Get("lines")
	lines := 100
	if l, err := strconv.Atoi(linesStr); err == nil && l > 0 {
		lines = l
	}
	logs, err := s.service.SecurityAuditLogs(request.Context(), lines)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"logs": logs})
}

func (s *Server) handleSecurityTrust(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	if err := s.service.Trust(request.Context()); err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	InvalidateReadModelCache(s.service)
	writeJSON(writer, http.StatusOK, map[string]string{"message": "Certificado raiz instalado e confiado."})
}

type commandRequest struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

func (s *Server) handleCommand(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input commandRequest
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "comando WSL inválido")
		return
	}
	input.Command = strings.ToLower(strings.TrimSpace(input.Command))
	for i := range input.Args {
		input.Args[i] = strings.TrimSpace(input.Args[i])
	}
	ctx, cancel := context.WithTimeout(request.Context(), 45*time.Second)
	defer cancel()

	response := map[string]any{"command": input.Command}
	switch input.Command {
	case "link":
		if len(input.Args) != 2 {
			writeJSONError(writer, http.StatusBadRequest, "uso: link NAME PATH")
			return
		}
		project, result, err := s.service.Link(ctx, input.Args[0], input.Args[1])
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		response["message"] = fmt.Sprintf("Projeto %s registrado: %s", project.Name, project.Path)
		response["warnings"] = result.Warnings
	case "unlink":
		if len(input.Args) != 1 {
			writeJSONError(writer, http.StatusBadRequest, "uso: unlink NAME")
			return
		}
		project, result, err := s.service.Unlink(ctx, input.Args[0])
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		response["message"] = fmt.Sprintf("Projeto %s removido do registro.", project.Name)
		response["warnings"] = result.Warnings
	case "park":
		if len(input.Args) == 2 && (input.Args[0] == "ignore" || input.Args[0] == "unignore") {
			var result app.ApplyResult
			var err error
			if input.Args[0] == "ignore" {
				result, err = s.service.IgnoreProject(ctx, input.Args[1])
			} else {
				result, err = s.service.UnignoreProject(ctx, input.Args[1])
			}
			if err != nil {
				writeJSONError(writer, http.StatusConflict, err.Error())
				return
			}
			response["message"] = fmt.Sprintf("Projeto %s: %s", input.Args[0], input.Args[1])
			response["warnings"] = result.Warnings
			break
		}
		if len(input.Args) != 1 {
			writeJSONError(writer, http.StatusBadRequest, "uso: park PATH | park ignore NAME|PATH | park unignore PATH")
			return
		}
		park, result, err := s.service.Park(ctx, input.Args[0])
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		response["message"] = fmt.Sprintf("Diretório estacionado: %s", park.Path)
		response["warnings"] = result.Warnings
	case "unpark":
		if len(input.Args) != 1 {
			writeJSONError(writer, http.StatusBadRequest, "uso: unpark PATH")
			return
		}
		park, result, err := s.service.Unpark(ctx, input.Args[0])
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		response["message"] = fmt.Sprintf("Diretório removido dos estacionados: %s", park.Path)
		response["warnings"] = result.Warnings
	case "links":
		if len(input.Args) > 1 {
			writeJSONError(writer, http.StatusBadRequest, "uso: links [FILTRO]")
			return
		}
		filter := ""
		if len(input.Args) == 1 {
			filter = input.Args[0]
		}
		views, err := BuildProjectViews(ctx, s.service, filter)
		if err != nil {
			writeJSONError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		response["projects"] = views
	case "status":
		if len(input.Args) != 0 {
			writeJSONError(writer, http.StatusBadRequest, "uso: status")
			return
		}
		statusView, err := BuildStatusView(ctx, s.service)
		if err != nil {
			writeJSONError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		response["status"] = statusView
	case "topology":
		if len(input.Args) > 1 || (len(input.Args) == 1 && input.Args[0] != "status" && input.Args[0] != "check") {
			writeJSONError(writer, http.StatusBadRequest, "uso: topology [status|check]")
			return
		}
		response["topology"] = s.service.CaddyTopologyStatus(ctx)
		response["caddy"] = s.service.CaddyStatus(ctx)
		if len(input.Args) == 1 && input.Args[0] == "check" {
			response["compatibility"] = s.service.WSLCompatibility(ctx)
		}
	case "reload":
		if len(input.Args) != 0 {
			writeJSONError(writer, http.StatusBadRequest, "uso: reload")
			return
		}
		result, err := s.service.Reload(ctx)
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		response["result"] = result
		response["message"] = "Configurações recarregadas."
	case "route":
		if len(input.Args) > 0 && input.Args[0] == "allocations" {
			if len(input.Args) == 1 {
				allocations, err := s.service.RouteAllocations(ctx)
				if err != nil {
					writeJSONError(writer, http.StatusInternalServerError, err.Error())
					return
				}
				response["allocations"] = allocations
				break
			}
			if len(input.Args) == 2 && input.Args[1] == "prune" {
				paths, result, err := s.service.PruneRouteAllocations(ctx, false)
				if err != nil {
					writeJSONError(writer, http.StatusConflict, err.Error())
					return
				}
				response["paths"] = paths
				response["result"] = result
				break
			}
			if len(input.Args) == 3 && input.Args[1] == "prune" && input.Args[2] == "--dry-run" {
				paths, result, err := s.service.PruneRouteAllocations(ctx, true)
				if err != nil {
					writeJSONError(writer, http.StatusInternalServerError, err.Error())
					return
				}
				response["paths"] = paths
				response["result"] = result
				break
			}
			writeJSONError(writer, http.StatusBadRequest, "uso: route allocations [prune [--dry-run]]")
			return
		}
		if len(input.Args) != 3 || input.Args[1] != "--port" {
			writeJSONError(writer, http.StatusBadRequest, "uso: route NAME --port auto|PORT")
			return
		}
		var port *int
		if input.Args[2] != "auto" {
			parsed, parseErr := strconv.Atoi(input.Args[2])
			if parseErr != nil || parsed < 1024 || parsed > 65535 {
				writeJSONError(writer, http.StatusBadRequest, "porta inválida")
				return
			}
			port = &parsed
		}
		result, err := s.service.SetRoutePort(ctx, input.Args[0], port)
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		response["result"] = result
		response["warnings"] = result.Warnings
		response["message"] = "Porta LAN atualizada."
	case "doctor":
		if len(input.Args) > 1 {
			writeJSONError(writer, http.StatusBadRequest, "uso: doctor [NAME]")
			return
		}
		name := ""
		if len(input.Args) == 1 {
			name = input.Args[0]
		}
		checks, err := s.service.Doctor(ctx, name)
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		response["checks"] = checks
	case "open":
		if len(input.Args) > 1 {
			writeJSONError(writer, http.StatusBadRequest, "uso: open [NAME]")
			return
		}
		name := ""
		if len(input.Args) == 1 {
			name = input.Args[0]
		}
		if name == "" {
			writeJSONError(writer, http.StatusBadRequest, "open no WSL exige NAME")
			return
		}
		url, err := s.service.Open(ctx, name)
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		response["url"] = url
		response["message"] = url
	default:
		writeJSONError(writer, http.StatusNotFound, "comando não permitido pelo protocolo WSL: "+input.Command)
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

func ensureToken(path string) (string, error) {
	if token, err := readToken(path); err == nil {
		return token, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	bytes := make([]byte, tokenSize)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("gerar token da API local: %w", err)
	}
	token := hex.EncodeToString(bytes)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return readToken(path)
		}
		return "", fmt.Errorf("criar token da API local: %w", err)
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("gravar token da API local: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	_ = os.Chmod(path, 0o600)
	return token, nil
}

func readToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if len(token) != tokenSize*2 {
		return "", fmt.Errorf("token da API local inválido")
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", fmt.Errorf("token da API local inválido: %w", err)
	}
	return token, nil
}

func writeEndpoint(path string, endpoint Endpoint) error {
	data, err := json.MarshalIndent(endpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar endpoint da API: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".devlan-api-endpoint-*.tmp")
	if err != nil {
		return fmt.Errorf("criar endpoint temporário: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(temporaryName, path)
}

func ReadEndpoint(endpointPath, tokenPath string) (Endpoint, error) {
	data, err := os.ReadFile(endpointPath)
	if err != nil {
		return Endpoint{}, err
	}
	var endpoint Endpoint
	if err := json.Unmarshal(data, &endpoint); err != nil {
		return Endpoint{}, fmt.Errorf("ler endpoint da API local: %w", err)
	}
	if endpoint.Version != ProtocolVersion || endpoint.Address == "" || !sameTokenPath(endpoint.TokenFile, tokenPath) {
		return Endpoint{}, ErrInvalidEndpoint
	}
	endpoint.TokenFile = tokenPath
	host, _, err := net.SplitHostPort(endpoint.Address)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		return Endpoint{}, ErrInvalidEndpoint
	}
	return endpoint, nil
}

func sameTokenPath(written, expected string) bool {
	if filepath.Clean(written) == filepath.Clean(expected) {
		return true
	}
	if runtime.GOOS != "linux" || len(written) < 3 || written[1] != ':' {
		return false
	}
	drive := strings.ToLower(string(written[0]))
	mounted := "/mnt/" + drive + strings.ReplaceAll(written[2:], "\\", "/")
	return filepath.Clean(mounted) == filepath.Clean(expected)
}

type Client struct {
	EndpointFile string
	TokenFile    string
	HTTPClient   *http.Client
}

// NewClient creates a local API client from the application-owned discovery
// files. CLI and tray callers never need direct access to config.Store.
func NewClient(service *app.App) Client {
	return NewClientFromFiles(service.APIEndpointFiles())
}

// NewClientForDataDir is for the thin WSL client, which only discovers the
// authenticated Windows endpoint and never opens a controller or state store.
func NewClientForDataDir(dataDir string) Client {
	return Client{
		EndpointFile: filepath.Join(dataDir, "api.endpoint.json"),
		TokenFile:    filepath.Join(dataDir, "api.token"),
	}
}

func NewClientFromFiles(files app.APIEndpointFiles) Client {
	return Client{EndpointFile: files.Endpoint, TokenFile: files.Token}
}

func (c Client) Command(ctx context.Context, command string, args []string) (map[string]any, error) {
	body, err := json.Marshal(commandRequest{Command: command, Args: args})
	if err != nil {
		return nil, err
	}
	response, err := c.Do(ctx, http.MethodPost, "/v1/command", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		if message, ok := payload["error"].(string); ok {
			return nil, errors.New(message)
		}
		return nil, fmt.Errorf("API local respondeu HTTP %d", response.StatusCode)
	}
	return payload, nil
}

func (c Client) Do(ctx context.Context, method, route string, body io.Reader) (*http.Response, error) {
	if !strings.HasPrefix(route, "/v1/") && !strings.HasPrefix(route, "/api/v1/") {
		return nil, fmt.Errorf("rota da API local inválida: %q", route)
	}
	endpoint, err := ReadEndpoint(c.EndpointFile, c.TokenFile)
	if err != nil {
		return nil, err
	}
	token, err := readToken(endpoint.TokenFile)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://"+endpoint.Address+route, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(request)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeJSONError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

func methodNotAllowed(writer http.ResponseWriter, allowed string) {
	writer.Header().Set("Allow", allowed)
	writeJSONError(writer, http.StatusMethodNotAllowed, "método HTTP não permitido")
}
