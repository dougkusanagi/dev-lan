package platform

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

type proxyTestManager struct {
	mu      sync.Mutex
	servers map[int]net.Listener
	starts  int
	stops   int
}

func (m *proxyTestManager) StartDev(_ context.Context, _ domain.Project, port int, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.starts++
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return err
	}
	if m.servers == nil {
		m.servers = map[int]net.Listener{}
	}
	m.servers[port] = listener
	go http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "ready")
	}))
	return nil
}

func (m *proxyTestManager) StopDev(_ context.Context, _ domain.Project, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stops++
	if listener := m.servers[port]; listener != nil {
		_ = listener.Close()
		delete(m.servers, port)
	}
	return nil
}
func (m *proxyTestManager) RestartDev(ctx context.Context, p domain.Project, port int, command string) error {
	_ = m.StopDev(ctx, p, port)
	return m.StartDev(ctx, p, port, command)
}
func (*proxyTestManager) Status(context.Context, domain.Project, int) (DevProcessStatus, error) {
	return DevProcessStatus{}, nil
}
func (*proxyTestManager) InstallDeps(context.Context, domain.Project, string) (string, error) {
	return "", nil
}
func (*proxyTestManager) Build(context.Context, domain.Project, string) (string, error) {
	return "", nil
}
func (*proxyTestManager) Logs(context.Context, domain.Project, int) (string, error) {
	return "", nil
}

func TestDevProxyColdStartsAndReapsIdleProcess(t *testing.T) {
	manager := &proxyTestManager{}
	proxy := NewDevProxy(manager)
	listenPort := freeTCPPort(t)
	project := domain.Project{Name: "vite", Path: t.TempDir()}
	if err := proxy.Ensure(context.Background(), project, listenPort, "npm run dev", 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	defer proxy.closeEntry(proxy.entries[project.Name])

	response, err := http.Get("http://127.0.0.1:" + strconv.Itoa(listenPort) + "/")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusServiceUnavailable || response.Header.Get("Retry-After") == "" {
		t.Fatalf("cold start deveria responder 503 com Retry-After, recebeu %d", response.StatusCode)
	}
	_ = response.Body.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response, err = http.Get("http://127.0.0.1:" + strconv.Itoa(listenPort) + "/")
		if err == nil {
			if response.StatusCode == http.StatusOK {
				_ = response.Body.Close()
				break
			}
			_ = response.Body.Close()
		}
		time.Sleep(20 * time.Millisecond)
	}
	manager.mu.Lock()
	starts := manager.starts
	manager.mu.Unlock()
	if starts != 1 {
		t.Fatalf("servidor deveria iniciar uma vez, iniciou %d", starts)
	}

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		manager.mu.Lock()
		stops := manager.stops
		manager.mu.Unlock()
		if stops > 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("servidor não foi encerrado após idle timeout")
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
