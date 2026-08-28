package app

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/config"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a

func (a *App) SetTLS(ctx context.Context, enabled bool) (ApplyResult, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	cfg.TLSEnabled = enabled
	result, err := a.saveAndApply(ctx, cfg, true)
	if err != nil {
		return result, err
	}
	if err := a.ensureFirewallSpec(ctx, cfg); err != nil && runtime.GOOS == "windows" {
		result.Warnings = append(result.Warnings, "não foi possível atualizar o firewall; execute o comando em PowerShell como Administrador")
	}
	if enabled {
		if err := a.Trust(ctx); err != nil {
			result.Warnings = append(result.Warnings, "não foi possível confiar na CA local automaticamente; execute `caddy trust` como Administrador")
		}
		_ = a.appendLog("TLS interno ativado")
	} else {
		_ = a.appendLog("TLS interno desativado")
	}
	return result, nil
}

func (a *App) Trust(ctx context.Context) error {
	if err := a.ensureInstallationManifest(ctx); err != nil {
		return err
	}
	caddyClient := a.edgeCaddy()
	if caddyClient.Runner == nil {
		return fmt.Errorf("Caddy WSL não configurado")
	}
	paths := a.Store.Paths()
	if caddyClient.WSL {
		if err := caddyClient.ExportRootCA(ctx, paths.CARootExport); err != nil {
			// A caller that explicitly injected the pre-M8 Windows client is
			// either running a compatibility test or performing a rollback. The
			// production constructor leaves WindowsCaddy empty, so this cannot
			// silently bring the old edge back into the normal install path.
			if a.WindowsCaddy.Runner != nil {
				return a.WindowsCaddy.Trust(ctx)
			}
			return err
		}
		trustedBefore := false
		if runtime.GOOS == "windows" {
			trustedBefore, _ = platform.CARootTrusted(ctx, paths.CARootExport)
		}
		if err := platform.InstallCARoot(ctx, paths.CARootExport); err != nil {
			return err
		}
		if runtime.GOOS == "windows" {
			if thumbprint, thumbprintErr := platform.CARootThumbprint(paths.CARootExport); thumbprintErr == nil {
				ownership := config.OwnershipCreated
				if trustedBefore {
					ownership = config.OwnershipPreexisting
				}
				if updateErr := a.Store.UpdateManifestResource("windows.ca-trust", func(resource *config.ManifestResource) {
					resource.Ownership = ownership
					resource.Fingerprint = thumbprint
					resource.Target = thumbprint
				}); updateErr != nil {
					return updateErr
				}
			}
		}
		return nil
	}
	// Compatibility for a pre-M8 controller that has not been migrated yet.
	return caddyClient.Trust(ctx)
}

// SetProjectTLS changes the HTTPS preference of one registered project. The
// Caddy WSL edge owns the certificate, while the selector keeps the command
// and advertised URL scoped to the requested project.
func (a *App) SetProjectTLS(ctx context.Context, selector string, enabled bool) (ApplyResult, string, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, "", err
	}
	selector = strings.TrimSpace(selector)
	effective, err := a.EffectiveConfig(ctx, cfg)
	if err != nil {
		return ApplyResult{}, "", err
	}
	selected, found := projectBySelector(effective.Projects, selector)
	if !found {
		return ApplyResult{}, "", fmt.Errorf("projeto não encontrado: %s", selector)
	}
	projectIndex := -1
	for i := range cfg.Projects {
		if cfg.Projects[i].Name == selected.Name || cfg.Projects[i].Path == selected.Path {
			projectIndex = i
			break
		}
	}
	if projectIndex < 0 {
		// A parked project is discovered rather than stored. Persist it once its
		// security preference becomes explicit, so the choice survives later
		// commands and does not require a separate `link` step.
		cfg.Projects = append(cfg.Projects, selected)
		projectIndex = len(cfg.Projects) - 1
	}
	cfg.Projects[projectIndex].Secure = &enabled
	if enabled {
		cfg.TLSEnabled = true
	}
	result, err := a.saveAndApply(ctx, cfg, true)
	if err != nil {
		return result, cfg.Projects[projectIndex].Name, err
	}
	if enabled {
		if err := a.ensureFirewallSpec(ctx, cfg); err != nil && runtime.GOOS == "windows" {
			result.Warnings = append(result.Warnings, "não foi possível atualizar o firewall; execute o comando em PowerShell como Administrador")
		}
		// Trust is machine state, not project state. It is intentionally kept out
		// of the TLS toggle critical path; the explicit Trust operation remains
		// available from the security/doctor UI.
	}
	return result, cfg.Projects[projectIndex].Name, nil
}

func projectBySelector(projects []domain.Project, selector string) (domain.Project, bool) {
	for _, project := range projects {
		if project.Name == selector || project.Path == selector {
			return project, true
		}
	}
	if normalized, err := domain.NormalizePath(selector); err == nil {
		for _, project := range projects {
			if project.Path == normalized {
				return project, true
			}
		}
	}
	return domain.Project{}, false
}
