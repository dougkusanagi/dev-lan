package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	WindowsCaddyAdminAddress = "127.0.0.1:2019"
	WSLCaddyAdminAddress     = "127.0.0.1:2020"
)

type CaddyClient struct {
	Runner Runner
	WSL    bool
	Binary string
}

func NewLocalCaddy(binary string) CaddyClient {
	if binary == "" {
		binary = FindLocalCaddy()
	}
	if binary == "" {
		binary = "caddy"
	}
	return CaddyClient{Runner: NewExecRunner(binary), Binary: binary}
}

func NewWSLCaddy(runner WSLRunner) CaddyClient {
	return CaddyClient{Runner: runner, WSL: true}
}

func (c CaddyClient) Available(ctx context.Context) error {
	if c.Runner == nil {
		return fmt.Errorf("%w: Caddy não configurado", ErrUnavailable)
	}
	var err error
	if c.WSL {
		_, err = c.Runner.Run(ctx, "caddy", "version")
	} else {
		// Local runners already contain the executable and expect only version.
		_, err = c.Runner.Run(ctx, "version")
	}
	return err
}

func (c CaddyClient) Validate(ctx context.Context, configPath string) error {
	if c.Runner == nil {
		return fmt.Errorf("%w: Caddy não configurado", ErrUnavailable)
	}
	if c.WSL {
		wslPath, err := ToWSLPath(configPath)
		if err != nil {
			return err
		}
		_, err = c.Runner.Run(ctx, "caddy", "validate", "--config", wslPath, "--adapter", "caddyfile")
		return err
	}
	_, err := c.Runner.Run(ctx, "validate", "--config", configPath, "--adapter", "caddyfile")
	return err
}

func (c CaddyClient) Reload(ctx context.Context, configPath string) error {
	if c.Runner == nil {
		return fmt.Errorf("%w: Caddy não configurado", ErrUnavailable)
	}
	if c.WSL {
		wslPath, err := ToWSLPath(configPath)
		if err != nil {
			return err
		}
		_, err = c.Runner.Run(ctx, "caddy", "reload", "--address", WSLCaddyAdminAddress, "--config", wslPath, "--adapter", "caddyfile")
		return err
	}
	_, err := c.Runner.Run(ctx, "reload", "--address", WindowsCaddyAdminAddress, "--config", configPath, "--adapter", "caddyfile")
	return err
}

// Trust installs Caddy's local root CA in the current Windows trust store.
// Caddy may require an elevated terminal depending on host policy.
func (c CaddyClient) Trust(ctx context.Context) error {
	if c.Runner == nil || c.WSL {
		return fmt.Errorf("%w: confiança TLS disponível apenas no Caddy Windows", ErrUnavailable)
	}
	_, err := c.Runner.Run(ctx, "trust")
	return err
}

func (c CaddyClient) HashPassword(ctx context.Context, password string) (string, error) {
	if c.Runner == nil {
		return "", fmt.Errorf("%w: Caddy não configurado", ErrUnavailable)
	}
	var out string
	var err error
	if c.WSL {
		out, err = c.Runner.Run(ctx, "caddy", "hash-password", "--plaintext", password)
	} else {
		out, err = c.Runner.Run(ctx, "hash-password", "--plaintext", password)
	}
	if err != nil {
		return "", fmt.Errorf("gerar hash de senha pelo Caddy: %w", err)
	}
	return out, nil
}

// EnsureRunning reloads a running Caddy or starts the local Windows instance
// after a reboot or a previously interrupted installation.
func (c CaddyClient) EnsureRunning(ctx context.Context, configPath string) error {
	if c.WSL {
		if err := c.Reload(ctx, configPath); err == nil {
			return nil
		}
		wslPath, err := ToWSLPath(configPath)
		if err != nil {
			return err
		}
		// The package normally installs Caddy as a service, but WSL services can
		// be unavailable after a reboot or when systemd is disabled. Start a
		// detached fallback so the GUI can recover without a terminal.
		script := fmt.Sprintf("nohup caddy run --config %s --adapter caddyfile >/tmp/devlan-caddy.log 2>&1 </dev/null &", shellQuote(wslPath))
		if _, err := c.Runner.Run(ctx, "/bin/sh", "-c", script); err != nil {
			return fmt.Errorf("iniciar Caddy no WSL: %w", err)
		}

		var lastErr error
		for attempt := 0; attempt < 20; attempt++ {
			if err := c.Reload(ctx, configPath); err == nil {
				return nil
			} else {
				lastErr = err
			}
			time.Sleep(250 * time.Millisecond)
		}
		return fmt.Errorf("Caddy no WSL não respondeu após iniciar: %w", lastErr)
	}
	if err := c.Reload(ctx, configPath); err == nil {
		return nil
	}
	if c.Binary == "" {
		return fmt.Errorf("%w: Caddy Windows não configurado", ErrUnavailable)
	}
	// `caddy start` waits for a pingback child process on Windows and can hold
	// the caller indefinitely. Start `run` detached with discarded output;
	// future operations use the admin API through Reload.
	command := exec.Command(c.Binary, "run", "--config", configPath, "--adapter", "caddyfile")
	command.Stdout = nil
	command.Stderr = nil
	hideProcessWindow(command)
	if err := command.Start(); err != nil {
		return fmt.Errorf("iniciar Caddy Windows: %w", err)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// FindLocalCaddy resolves both a PATH installation and Caddy installed by
// WinGet, whose package directory is not always present in the current PATH.
func FindLocalCaddy() string {
	if path, err := exec.LookPath("caddy.exe"); err == nil {
		return path
	}
	if path, err := exec.LookPath("caddy"); err == nil {
		return path
	}
	if runtime.GOOS != "windows" {
		return ""
	}
	var candidates []string
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		matches, _ := filepath.Glob(filepath.Join(localAppData, "Microsoft", "WinGet", "Packages", "CaddyServer.Caddy_*", "caddy.exe"))
		candidates = append(candidates, matches...)
	}
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		candidates = append(candidates, filepath.Join(programFiles, "Caddy", "caddy.exe"))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
