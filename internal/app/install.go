package app

import (
	"context"
	"runtime"

	"github.com/dougkusanagi/dev-lan/internal/platform"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a

func (a *App) CloseDevProxies() error {
	if a.DevProxy == nil {
		return nil
	}
	return a.DevProxy.Close()
}

func (a *App) Install(ctx context.Context) (ApplyResult, error) {
	return a.InstallWithOptions(ctx, true)
}

func (a *App) InstallWithOptions(ctx context.Context, configureFirewall bool) (ApplyResult, error) {
	return a.InstallWithPort(ctx, configureFirewall, 0)
}

func (a *App) InstallWithPort(ctx context.Context, configureFirewall bool, windowsPort int) (ApplyResult, error) {
	ctx = platform.WithWSLOperation(ctx, platform.WSLOperationInstall)
	if err := a.ensureInstallationManifest(ctx); err != nil {
		return ApplyResult{}, err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	if windowsPort != 0 {
		cfg.WindowsPort = windowsPort
	}
	result, err := a.saveAndApplyMode(ctx, cfg, false, BootstrapTolerant)
	if err != nil {
		return result, err
	}
	// Caddy may have created its WSL provenance marker while applying the
	// generated config. Refresh the manifest after that side effect as well as
	// before the transaction, so a direct `devlan install` is uninstallable.
	if manifestErr := a.ensureInstallationManifest(ctx); manifestErr != nil {
		result.Warnings = append(result.Warnings, "não foi possível atualizar o manifesto de instalação: "+manifestErr.Error())
	}
	if fingerprintErr := a.refreshWSLManagedFingerprints(ctx, "wsl.caddy-config"); fingerprintErr != nil {
		result.Warnings = append(result.Warnings, "não foi possível atualizar a fingerprint do Caddy WSL: "+fingerprintErr.Error())
	}
	if configureFirewall {
		if err := a.ensureFirewallSpec(ctx, cfg); err != nil && runtime.GOOS == "windows" {
			result.Warnings = append(result.Warnings, "não foi possível reconciliar Windows Firewall/Hyper-V Firewall; execute install como administrador")
		}
	}
	if err := a.edgeCaddy().Available(ctx); err != nil {
		result.Warnings = append(result.Warnings, "Caddy único no WSL não encontrado; instale-o e execute devlan doctor")
	}
	phpCommands := []string{"php-fpm", "php-fpm8.5", "php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.1"}
	phpFound := false
	if found, findErr := a.WSL.HasCommands(ctx, phpCommands...); findErr == nil {
		for _, command := range phpCommands {
			if found[command] {
				phpFound = true
				break
			}
		}
	}
	if !phpFound {
		result.Warnings = append(result.Warnings, "PHP-FPM não encontrado no WSL; instale uma versão suportada e execute devlan doctor")
	}
	result.Status = statusFor(result)
	_ = a.appendLog("install concluído")
	a.recordTelemetry("install", map[string]string{"result": "ok"})
	return result, nil
}
