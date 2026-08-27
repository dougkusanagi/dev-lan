package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// SetAuth persists only a Caddy-produced password hash. In particular, a
// failed Caddy invocation cannot turn a transient runtime fault into a
// plaintext credential on disk.
func (a *App) SetAuth(ctx context.Context, selector string, enabled bool, username, password string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	var hash string
	if password != "" {
		caddyClient := a.edgeCaddy()
		if caddyClient.Runner == nil {
			return ApplyResult{}, ErrPasswordHashUnavailable
		}
		h, hashErr := caddyClient.HashPassword(ctx, password)
		if hashErr != nil {
			return ApplyResult{}, fmt.Errorf("%w: %v", ErrPasswordHashUnavailable, hashErr)
		}
		hash = strings.TrimSpace(h)
		if hash == "" {
			return ApplyResult{}, ErrPasswordHashUnavailable
		}
	}
	user := domain.AuthUser{Username: username, PasswordHash: hash}
	if selector == "default" || selector == "" {
		if username != "" {
			cfg.AuthUsers = []domain.AuthUser{user}
		}
	} else {
		cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
		if err != nil {
			return ApplyResult{}, err
		}
		cfg.Projects[index].AuthEnabled = &enabled
		if username != "" {
			cfg.Projects[index].AuthUsers = []domain.AuthUser{user}
		}
		selector = name
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("auth %s enabled=%t user=%s", selector, enabled, username)
		_ = a.Store.AppendSecurityAudit("AUTH_SET", fmt.Sprintf("target=%s enabled=%t user=%s", selector, enabled, username))
	}
	return result, err
}

func (a *App) DisableAuth(ctx context.Context, selector string) (ApplyResult, error) {
	disabled := false
	return a.SetAuth(ctx, selector, disabled, "", "")
}

// MigrateLegacyAuth hashes credentials written by older releases before the
// fail-closed rule existed. It performs no write until every legacy value has
// been converted successfully, so a Caddy failure leaves the prior state
// intact rather than mixing plaintext and hashes.
func (a *App) MigrateLegacyAuth(ctx context.Context) (ApplyResult, error) {
	cfg, err := a.Config()
	if err != nil {
		return ApplyResult{}, err
	}
	caddyClient := a.edgeCaddy()
	if caddyClient.Runner == nil {
		return ApplyResult{}, ErrPasswordHashUnavailable
	}
	migrate := func(users []domain.AuthUser) ([]domain.AuthUser, bool, error) {
		converted := append([]domain.AuthUser(nil), users...)
		changed := false
		for index := range converted {
			if isCaddyPasswordHash(converted[index].PasswordHash) {
				continue
			}
			hash, hashErr := caddyClient.HashPassword(ctx, converted[index].PasswordHash)
			if hashErr != nil || !isCaddyPasswordHash(hash) {
				if hashErr != nil {
					return nil, false, fmt.Errorf("%w: %v", ErrPasswordHashUnavailable, hashErr)
				}
				return nil, false, ErrPasswordHashUnavailable
			}
			converted[index].PasswordHash = strings.TrimSpace(hash)
			changed = true
		}
		return converted, changed, nil
	}

	users, changed, err := migrate(cfg.AuthUsers)
	if err != nil {
		return ApplyResult{}, err
	}
	cfg.AuthUsers = users
	for index := range cfg.Projects {
		users, projectChanged, migrateErr := migrate(cfg.Projects[index].AuthUsers)
		if migrateErr != nil {
			return ApplyResult{}, migrateErr
		}
		cfg.Projects[index].AuthUsers = users
		changed = changed || projectChanged
	}
	if !changed {
		return ApplyResult{Revision: cfg.Revision, Status: "unchanged"}, nil
	}
	result, err := a.SaveConfigAndApply(ctx, cfg, true)
	if err == nil {
		a.Audit("AUTH_MIGRATED", "credenciais legadas migradas para hash Caddy")
	}
	return result, err
}

func isCaddyPasswordHash(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "$2a$") ||
		strings.HasPrefix(strings.TrimSpace(value), "$2b$") ||
		strings.HasPrefix(strings.TrimSpace(value), "$2y$")
}
