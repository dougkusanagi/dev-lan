package platform

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

type recordingRunner struct {
	args []string
}

func (r *recordingRunner) Run(_ context.Context, args ...string) (string, error) {
	r.args = append([]string(nil), args...)
	return "", nil
}

func TestLocalCaddyReloadTargetsWindowsAdmin(t *testing.T) {
	runner := &recordingRunner{}
	client := CaddyClient{Runner: runner}
	if err := client.Reload(context.Background(), `C:\DevLAN\Caddyfile.windows`); err != nil {
		t.Fatal(err)
	}
	want := []string{"reload", "--address", WindowsCaddyAdminAddress, "--config", `C:\DevLAN\Caddyfile.windows`, "--adapter", "caddyfile"}
	if !slices.Equal(runner.args, want) {
		t.Fatalf("argumentos inesperados: %q", runner.args)
	}
}

func TestWSLCaddyReloadTargetsWSLAdmin(t *testing.T) {
	runner := &recordingRunner{}
	client := CaddyClient{Runner: runner, WSL: true}
	if err := client.Reload(context.Background(), `C:\DevLAN\Caddyfile.wsl`); err != nil {
		t.Fatal(err)
	}
	want := []string{"caddy", "reload", "--address", WSLCaddyAdminAddress, "--config", "/mnt/c/DevLAN/Caddyfile.wsl", "--adapter", "caddyfile"}
	if !slices.Equal(runner.args, want) {
		t.Fatalf("argumentos inesperados: %q", runner.args)
	}
}

type statusRunner struct{}

func (statusRunner) Run(_ context.Context, args ...string) (string, error) {
	if strings.Contains(strings.Join(args, " "), "is-active") {
		return "inactive\n", errors.New("inactive")
	}
	return "", nil
}

func TestWSLCaddyStatusDoesNotPromoteStrayAdminEndpoint(t *testing.T) {
	client := CaddyClient{
		Runner:         statusRunner{},
		WSL:            true,
		RequireSystemd: true,
		AdminProbe:     func(context.Context, string) bool { return true },
	}
	status := client.Status(context.Background())
	if !status.Available || !status.Systemd || status.Running || !status.Live {
		t.Fatalf("endpoint admin órfão não deveria parecer serviço systemd ativo: %#v", status)
	}
}
