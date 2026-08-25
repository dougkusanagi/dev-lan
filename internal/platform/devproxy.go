package platform

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// DevProxy keeps a stable Caddy-facing port while starting the real JS
// process only after the first request. The gateway remains alive during the
// idle period, so a later request can cold-start the project again.
type DevProxy struct {
	manager DevManager

	mu      sync.Mutex
	entries map[string]*devProxyEntry
}

type devProxyEntry struct {
	project   domain.Project
	listen    int
	backend   int
	command   string
	idle      time.Duration
	listener  net.Listener
	server    *http.Server
	done      chan struct{}
	closeOnce sync.Once

	starting bool
	running  bool
	lastUse  time.Time
	startErr error
}

func NewDevProxy(manager DevManager) *DevProxy {
	return &DevProxy{manager: manager, entries: make(map[string]*devProxyEntry)}
}

func (p *DevProxy) entryFor(name string) *devProxyEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.entries[name]
}

// StartNow preserves the explicit `devlan start` behavior when a gateway is
// already registered: it starts the backend, never the Caddy-facing port.
func (p *DevProxy) StartNow(ctx context.Context, project domain.Project, listenPort int, command string, idle time.Duration) error {
	if p.entryFor(project.Name) == nil {
		if err := p.Ensure(ctx, project, listenPort, command, idle); err != nil {
			return err
		}
	}
	entry := p.entryFor(project.Name)
	if entry == nil {
		return fmt.Errorf("gateway dev %s não foi registrado", project.Name)
	}
	p.mu.Lock()
	if entry.running {
		p.mu.Unlock()
		return nil
	}
	if entry.starting {
		p.mu.Unlock()
		return nil
	}
	entry.starting = true
	p.mu.Unlock()
	p.start(entry)
	p.mu.Lock()
	err := entry.startErr
	p.mu.Unlock()
	if err != nil {
		return err
	}
	return nil
}

func (p *DevProxy) StopNow(ctx context.Context, name string) error {
	entry := p.entryFor(name)
	if entry == nil {
		return nil
	}
	p.mu.Lock()
	running := entry.running
	p.mu.Unlock()
	if !running {
		return nil
	}
	if err := p.manager.StopDev(ctx, entry.project, entry.backend); err != nil {
		return err
	}
	p.mu.Lock()
	entry.running = false
	entry.startErr = nil
	entry.lastUse = time.Now()
	p.mu.Unlock()
	return nil
}

func (p *DevProxy) Status(name string) (running, starting bool) {
	entry := p.entryFor(name)
	if entry == nil {
		return false, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return entry.running, entry.starting
}

// Close stops all backend processes and gateway listeners owned by this
// supervisor. It is called by the service/UI lifecycle before shutdown.
func (p *DevProxy) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	entries := make([]*devProxyEntry, 0, len(p.entries))
	for _, entry := range p.entries {
		entries = append(entries, entry)
	}
	p.mu.Unlock()
	var firstErr error
	for _, entry := range entries {
		if err := p.closeEntry(entry); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Ensure registers a gateway once. Calling it repeatedly during reload is
// idempotent and updates the command/idle policy for an existing project.
func (p *DevProxy) Ensure(ctx context.Context, project domain.Project, listenPort int, command string, idle time.Duration) error {
	_ = ctx
	if p == nil || p.manager == nil {
		return fmt.Errorf("supervisor dev não configurado")
	}
	if listenPort < 1024 || listenPort > 65535 {
		return fmt.Errorf("porta do proxy dev inválida: %d", listenPort)
	}
	if idle <= 0 {
		idle = 15 * time.Minute
	}
	backend := devBackendPort(listenPort)

	p.mu.Lock()
	if existing := p.entries[project.Name]; existing != nil {
		existing.command = command
		existing.idle = idle
		existing.project = project
		p.mu.Unlock()
		return nil
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", listenPort))
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("abrir gateway dev %s na porta %d: %w", project.Name, listenPort, err)
	}
	entry := &devProxyEntry{
		project: project, listen: listenPort, backend: backend, command: command,
		idle: idle, listener: listener, lastUse: time.Now(),
		done: make(chan struct{}),
	}
	entry.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.serve(entry, w, r)
	})}
	p.entries[project.Name] = entry
	p.mu.Unlock()

	go func() {
		if err := entry.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			p.mu.Lock()
			entry.startErr = err
			p.mu.Unlock()
		}
	}()
	// The reload context is short-lived; the gateway itself must outlive that
	// request and remain available until its owning process shuts down.
	go p.reap(context.Background(), entry)
	return nil
}

func devBackendPort(listen int) int {
	if listen <= 55000 {
		return listen + 10000
	}
	return listen - 1000
}

func (p *DevProxy) serve(entry *devProxyEntry, writer http.ResponseWriter, request *http.Request) {
	p.mu.Lock()
	entry.lastUse = time.Now()
	running, starting, startErr := entry.running, entry.starting, entry.startErr
	if !running && !starting {
		entry.starting = true
		starting = true
		go p.start(entry)
	}
	p.mu.Unlock()

	if startErr != nil {
		writer.Header().Set("Retry-After", "5")
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintf(writer, "<h1>Servidor dev indisponível</h1><p>%s</p>", htmlEscape(startErr.Error()))
		return
	}
	if starting || !running {
		writer.Header().Set("Retry-After", "2")
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte("<!doctype html><meta charset=utf-8><title>DevLAN</title><h1>Iniciando servidor de desenvolvimento…</h1><p>Tente novamente em instantes.</p>"))
		return
	}

	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", entry.backend))
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		w.Header().Set("Retry-After", "2")
		http.Error(w, "servidor dev ainda não está pronto: "+err.Error(), http.StatusServiceUnavailable)
	}
	proxy.Director = func(out *http.Request) {
		out.URL.Scheme = target.Scheme
		out.URL.Host = target.Host
		out.Host = request.Host
		out.Header.Set("X-Forwarded-Host", request.Host)
	}
	proxy.ServeHTTP(writer, request)
}

func (p *DevProxy) start(entry *devProxyEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := p.manager.StartDev(ctx, entry.project, entry.backend, entry.command)
	p.mu.Lock()
	entry.starting = false
	entry.startErr = err
	entry.running = err == nil
	entry.lastUse = time.Now()
	p.mu.Unlock()
}

func (p *DevProxy) reap(ctx context.Context, entry *devProxyEntry) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-entry.done:
			return
		case <-ctx.Done():
			_ = p.closeEntry(entry)
			return
		case <-ticker.C:
			p.mu.Lock()
			expired := entry.running && !entry.starting && time.Since(entry.lastUse) >= entry.idle
			p.mu.Unlock()
			if expired {
				stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = p.manager.StopDev(stopCtx, entry.project, entry.backend)
				cancel()
				p.mu.Lock()
				entry.running = false
				entry.startErr = nil
				entry.lastUse = time.Now()
				p.mu.Unlock()
			}
		}
	}
}

func (p *DevProxy) closeEntry(entry *devProxyEntry) error {
	p.mu.Lock()
	if entry.running {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = p.manager.StopDev(ctx, entry.project, entry.backend)
		cancel()
	}
	delete(p.entries, entry.project.Name)
	server := entry.server
	p.mu.Unlock()
	entry.closeOnce.Do(func() { close(entry.done) })
	if server != nil {
		return server.Close()
	}
	return nil
}

func htmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	return strings.ReplaceAll(value, "\"", "&quot;")
}
