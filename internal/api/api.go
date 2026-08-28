// Package api exposes the loopback web server and authenticated API
// shared by the browser GUI, Windows service, CLI and WSL clients.
package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
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
