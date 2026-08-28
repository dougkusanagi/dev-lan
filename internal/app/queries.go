package app

import (
	"context"
	"fmt"

	"github.com/dougkusanagi/dev-lan/internal/config"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// Config returns the authoritative configuration snapshot to application
// queries. Transports use this boundary instead of reaching into Store.
func (a *App) Config() (domain.Config, error) {
	return a.Store.Load()
}

// EnsureState creates the DevLAN-managed state directories before a transport
// starts. Persistence remains owned by the application service.
func (a *App) EnsureState() error {
	return a.Store.Ensure()
}

// APIEndpointFiles returns only the local API discovery files required by
// transports and helper processes.
func (a *App) APIEndpointFiles() APIEndpointFiles {
	paths := a.Store.Paths()
	return APIEndpointFiles{Endpoint: paths.APIEndpoint, Token: paths.APIToken}
}

// ManagedPaths exposes derived managed artifact locations to read-only views.
// Callers must not mutate these files or use this as a persistence API.
func (a *App) ManagedPaths() config.Paths {
	return a.Store.Paths()
}

// Revision returns zero only when the authoritative state cannot be read.
func (a *App) Revision() uint64 {
	cfg, err := a.Config()
	if err != nil {
		return 0
	}
	return cfg.Revision
}

// Audit records transport lifecycle events without exposing persistence to a
// caller such as the HTTP server or the optional Wails shell.
func (a *App) Audit(event, details string) {
	_ = a.Store.AppendSecurityAudit(event, details)
}

// SaveGlobalSettings applies the validated global settings command owned by
// internal/application. It deliberately does not expose persistence records.
func (a *App) SaveGlobalSettings(ctx context.Context, settings GlobalSettings) (ApplyResult, error) {
	cfg, err := a.Config()
	if err != nil {
		return ApplyResult{}, err
	}
	if settings.DefaultMode != "" {
		mode, parseErr := domain.ParseMode(settings.DefaultMode)
		if parseErr != nil {
			return ApplyResult{}, parseErr
		}
		cfg.DefaultMode = mode
	}
	if settings.WindowsPort > 0 {
		cfg.WindowsPort = settings.WindowsPort
	}
	if settings.HTTPSPort > 0 {
		cfg.HTTPSPort = settings.HTTPSPort
	}
	cfg.TLSEnabled = settings.TLSEnabled
	if settings.PHPDefaultVersion != "" {
		cfg.PHPDefaultVersion = settings.PHPDefaultVersion
	}
	if err := cfg.SetGlobalAllowlist(settings.Allowlist); err != nil {
		return ApplyResult{}, fmt.Errorf("allowlist global: %w", err)
	}
	return a.SaveConfigAndApply(ctx, cfg, true)
}
