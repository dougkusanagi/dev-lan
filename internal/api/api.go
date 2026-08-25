// Package api exposes the authenticated loopback protocol shared by the
// optional Windows service, the CLI and future UI clients.
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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/config"
)

const (
	ProtocolVersion = 1
	tokenSize       = 32
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
	store   config.Store

	mu         sync.Mutex
	listener   net.Listener
	httpServer *http.Server
	endpoint   Endpoint
}

func New(service *app.App) *Server {
	return &Server{service: service, store: service.Store}
}

func (s *Server) Start() (Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.endpoint, nil
	}
	if err := s.store.Ensure(); err != nil {
		return Endpoint{}, err
	}
	if _, err := os.Stat(s.store.Paths().APIEndpoint); err == nil {
		if endpoint, readErr := ReadEndpoint(s.store); readErr == nil {
			checkContext, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			response, requestErr := (Client{Store: s.store}).Do(checkContext, http.MethodGet, "/v1/health", nil)
			cancel()
			if response != nil {
				_ = response.Body.Close()
			}
			if requestErr == nil && response != nil && response.StatusCode < 400 {
				return endpoint, ErrAlreadyRunning
			}
		}
		_ = os.Remove(s.store.Paths().APIEndpoint)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Endpoint{}, err
	}
	token, err := ensureToken(s.store.Paths().APIToken)
	if err != nil {
		return Endpoint{}, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Endpoint{}, fmt.Errorf("iniciar API local: %w", err)
	}
	endpoint := Endpoint{
		Version:   ProtocolVersion,
		Address:   listener.Addr().String(),
		TokenFile: s.store.Paths().APIToken,
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	server := &http.Server{Handler: s.Handler(token)}
	if err := writeEndpoint(s.store.Paths().APIEndpoint, endpoint); err != nil {
		_ = listener.Close()
		return Endpoint{}, err
	}
	s.listener = listener
	s.httpServer = server
	s.endpoint = endpoint
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = s.service.Store.AppendSecurityAudit("API_STOP", "servidor local encerrado: "+err.Error())
		}
	}()
	_ = s.service.Store.AppendSecurityAudit("API_START", "API local autenticada em "+endpoint.Address)
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
	endpointPath := s.store.Paths().APIEndpoint
	s.listener = nil
	s.httpServer = nil
	s.endpoint = Endpoint{}
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	err := server.Shutdown(ctx)
	_ = os.Remove(endpointPath)
	_ = s.service.Store.AppendSecurityAudit("API_STOP", "API local encerrada")
	return err
}

func (s *Server) Handler(token ...string) http.Handler {
	secret := ""
	if len(token) > 0 {
		secret = token[0]
	} else {
		loaded, err := readToken(s.store.Paths().APIToken)
		if err == nil {
			secret = loaded
		}
	}
	mux := http.NewServeMux()
	guard := func(handler http.HandlerFunc) http.HandlerFunc {
		return func(writer http.ResponseWriter, request *http.Request) {
			if !authorized(request, secret) {
				writeJSONError(writer, http.StatusUnauthorized, "token da API local ausente ou inválido")
				return
			}
			if request.Body != nil {
				request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
			}
			handler(writer, request)
		}
	}
	mux.HandleFunc("/v1/health", guard(s.handleHealth))
	mux.HandleFunc("/v1/status", guard(s.handleStatus))
	mux.HandleFunc("/v1/projects", guard(s.handleProjects))
	mux.HandleFunc("/v1/config", guard(s.handleConfig))
	mux.HandleFunc("/v1/reload", guard(s.handleReload))
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		writeJSONError(writer, http.StatusNotFound, "rota da API local não encontrada")
	})
	return restrictLoopback(mux)
}

func restrictLoopback(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		host, _, err := net.SplitHostPort(request.RemoteAddr)
		if err != nil || (host != "127.0.0.1" && host != "::1") {
			writeJSONError(writer, http.StatusForbidden, "API local aceita somente loopback")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

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
	cfg, err := s.store.Load()
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"runtime":          runtime.GOOS + "/" + runtime.GOARCH,
		"data_dir":         s.store.Paths().Dir,
		"default_mode":     cfg.DefaultMode,
		"default_route":    cfg.DefaultRouteMode,
		"windows_port":     cfg.WindowsPort,
		"https_port":       cfg.HTTPSPort,
		"tls_enabled":      cfg.TLSEnabled,
		"project_count":    len(cfg.Projects),
		"park_count":       len(cfg.Parks),
		"protocol_version": ProtocolVersion,
	})
}

func (s *Server) handleProjects(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	cfg, err := s.store.Load()
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	safe, err := config.SanitizeForExport(cfg)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, safe.Projects)
}

func (s *Server) handleConfig(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
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
	writeJSON(writer, http.StatusOK, result)
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

func ReadEndpoint(store config.Store) (Endpoint, error) {
	data, err := os.ReadFile(store.Paths().APIEndpoint)
	if err != nil {
		return Endpoint{}, err
	}
	var endpoint Endpoint
	if err := json.Unmarshal(data, &endpoint); err != nil {
		return Endpoint{}, fmt.Errorf("ler endpoint da API local: %w", err)
	}
	if endpoint.Version != ProtocolVersion || endpoint.Address == "" || endpoint.TokenFile != store.Paths().APIToken {
		return Endpoint{}, ErrInvalidEndpoint
	}
	host, _, err := net.SplitHostPort(endpoint.Address)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		return Endpoint{}, ErrInvalidEndpoint
	}
	return endpoint, nil
}

type Client struct {
	Store      config.Store
	HTTPClient *http.Client
}

func (c Client) Do(ctx context.Context, method, route string, body io.Reader) (*http.Response, error) {
	if !strings.HasPrefix(route, "/v1/") {
		return nil, fmt.Errorf("rota da API local inválida: %q", route)
	}
	endpoint, err := ReadEndpoint(c.Store)
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
