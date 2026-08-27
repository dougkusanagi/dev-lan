package app

import (
	"context"
	"fmt"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// Config returns the authoritative configuration snapshot to application
// queries. Transports use this boundary instead of reaching into Store.
func (a *App) Config() (domain.Config, error) {
	return a.Store.Load()
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

// GlobalSettings is the command DTO shared by the HTTP and Wails transports.
// It deliberately does not expose persistence records.
type GlobalSettings struct {
	DefaultMode       string
	WindowsPort       int
	HTTPSPort         int
	TLSEnabled        bool
	PHPDefaultVersion string
	Allowlist         []string
}

// SaveGlobalSettings applies a validated global settings command.
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
