package caddy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestRealCaddyPair is opt-in because the normal unit-test environment does
// not necessarily have Caddy installed. CI enables it on the integration job.
// It intentionally starts two independent Caddy processes on ephemeral ports;
// string-only renderer tests cannot detect lifecycle or binding regressions.
func TestRealCaddyPair(t *testing.T) {
	if os.Getenv("DEVLAN_RUN_CADDY_TESTS") != "1" {
		t.Skip("defina DEVLAN_RUN_CADDY_TESTS=1 para o smoke com dois Caddys reais")
	}
	caddy, err := exec.LookPath("caddy")
	if err != nil {
		t.Skip("Caddy não instalado")
	}
	first := freePort(t)
	second := freePort(t)
	dir := t.TempDir()
	staticRoot, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fixtures", "static"))
	if err != nil {
		t.Fatal(err)
	}
	configs := []string{
		fmt.Sprintf("{\n  admin off\n}\nhttp://127.0.0.1:%d {\n  root * %q\n  respond /health 200\n  file_server\n}\n", first, staticRoot),
		fmt.Sprintf("{\n  admin off\n}\nhttp://127.0.0.1:%d {\n  root * %q\n  respond /health 200\n  file_server\n}\n", second, staticRoot),
	}
	commands := make([]*exec.Cmd, 0, 2)
	for i, config := range configs {
		path := filepath.Join(dir, fmt.Sprintf("Caddyfile.%d", i))
		if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		validate := exec.Command(caddy, "validate", "--config", path, "--adapter", "caddyfile")
		if output, err := validate.CombinedOutput(); err != nil {
			t.Fatalf("Caddyfile %d inválido: %v\n%s", i, err, output)
		}
		cmd := exec.Command(caddy, "run", "--config", path, "--adapter", "caddyfile")
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	t.Cleanup(func() {
		for _, cmd := range commands {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for _, port := range []int{first, second} {
		waitForHTTP(t, client, fmt.Sprintf("http://127.0.0.1:%d/health", port))
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK || len(body) == 0 {
			t.Fatalf("fixture não servida pelo Caddy %d: status=%d err=%v", port, response.StatusCode, readErr)
		}
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func waitForHTTP(t *testing.T, client *http.Client, target string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		response, err := client.Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Caddy não respondeu em %s", target)
}
