package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// EnsureRunning reloads a running Caddy or starts the local Windows instance
// after a reboot or a previously interrupted installation.
func (c CaddyClient) EnsureRunning(ctx context.Context, configPath string) error {
	if c.WSL {
		return c.Reload(ctx, configPath)
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
	if err := command.Start(); err != nil {
		return fmt.Errorf("iniciar Caddy Windows: %w", err)
	}
	return nil
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
