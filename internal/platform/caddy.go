package platform

import (
	"context"
	"fmt"
)

const (
	WindowsCaddyAdminAddress = "127.0.0.1:2019"
	WSLCaddyAdminAddress     = "127.0.0.1:2020"
)

type CaddyClient struct {
	Runner Runner
	WSL    bool
}

func NewLocalCaddy(binary string) CaddyClient {
	if binary == "" {
		binary = "caddy"
	}
	return CaddyClient{Runner: NewExecRunner(binary)}
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
