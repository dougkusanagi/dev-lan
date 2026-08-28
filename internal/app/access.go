package app

import (
	"context"
	"fmt"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a

func (a *App) SetAllowlist(ctx context.Context, selector string, cidrs []string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	if selector == "default" || selector == "" {
		if err := cfg.SetGlobalAllowlist(cidrs); err != nil {
			return ApplyResult{}, err
		}
	} else {
		cfg, name, _, err := a.materializeProject(ctx, cfg, selector)
		if err != nil {
			return ApplyResult{}, err
		}
		if err := cfg.SetProjectAllowlist(name, cidrs); err != nil {
			return ApplyResult{}, err
		}
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog(fmt.Sprintf("allowlist %s atualizada (%d CIDRs)", selector, len(cidrs)))
		_ = a.Store.AppendSecurityAudit("ALLOWLIST_SET", fmt.Sprintf("target=%s cidrs=%v", selector, cidrs))
	}
	return result, err
}

func (a *App) AddAllowlist(ctx context.Context, selector string, cidrs []string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	var current []string
	if selector == "default" || selector == "" {
		current = append([]string(nil), cfg.Allowlist...)
	} else {
		cfg, name, idx, err := a.materializeProject(ctx, cfg, selector)
		if err != nil {
			return ApplyResult{}, err
		}
		current = append([]string(nil), cfg.Projects[idx].Allowlist...)
		selector = name
	}
	current = append(current, cidrs...)
	return a.SetAllowlist(ctx, selector, current)
}

func (a *App) RemoveAllowlist(ctx context.Context, selector string, cidrs []string) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	toRemove := map[string]bool{}
	for _, c := range cidrs {
		norm, _ := domain.NormalizeCIDR(c)
		if norm != "" {
			toRemove[norm] = true
		}
		toRemove[c] = true
	}
	var current []string
	if selector == "default" || selector == "" {
		current = cfg.Allowlist
	} else {
		cfg, name, idx, err := a.materializeProject(ctx, cfg, selector)
		if err != nil {
			return ApplyResult{}, err
		}
		current = cfg.Projects[idx].Allowlist
		selector = name
	}
	var filtered []string
	for _, item := range current {
		if !toRemove[item] {
			filtered = append(filtered, item)
		}
	}
	return a.SetAllowlist(ctx, selector, filtered)
}

func (a *App) ClearAllowlist(ctx context.Context, selector string) (ApplyResult, error) {
	return a.SetAllowlist(ctx, selector, []string{})
}

func (a *App) ExposeProject(ctx context.Context, selector string, duration time.Duration) (ApplyResult, string, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, "", err
	}
	cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
	if err != nil {
		return ApplyResult{}, "", err
	}
	var untilStr *string
	if duration > 0 {
		exp := a.now().Add(duration).UTC().Format(time.RFC3339)
		untilStr = &exp
	}
	cfg.Projects[index].ExposedUntil = untilStr
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog(fmt.Sprintf("expose %s duration=%v", name, duration))
		_ = a.Store.AppendSecurityAudit("EXPOSE_PROJECT", fmt.Sprintf("project=%s duration=%v until=%v", name, duration, untilStr))
	}
	return result, name, err
}

func (a *App) UnexposeProject(ctx context.Context, selector string) (ApplyResult, string, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, "", err
	}
	cfg, name, index, err := a.materializeProject(ctx, cfg, selector)
	if err != nil {
		return ApplyResult{}, "", err
	}
	past := a.now().Add(-1 * time.Minute).UTC().Format(time.RFC3339)
	cfg.Projects[index].ExposedUntil = &past
	result, err := a.saveAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog(fmt.Sprintf("unexpose %s", name))
		_ = a.Store.AppendSecurityAudit("UNEXPOSE_PROJECT", fmt.Sprintf("project=%s", name))
	}
	return result, name, err
}
