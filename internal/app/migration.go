package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/platform"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a

func (a *App) MigrateToSingleCaddy(ctx context.Context, confirmed bool) (platform.MigrationResult, error) {
	if !confirmed {
		return platform.MigrationResult{}, platform.ErrWSLShutdownConfirmation
	}
	a.topologyMu.Lock()
	defer a.topologyMu.Unlock()
	if err := a.ensureInstallationManifest(ctx); err != nil {
		return platform.MigrationResult{}, err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return platform.MigrationResult{}, err
	}
	// The unified edge exposes its listeners directly through mirrored WSL.
	// Reconcile the host and Hyper-V policies before the port handoff so a
	// non-elevated migration fails while the previous Caddy is still serving.
	if os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		if err := a.ensureFirewallSpec(ctx, cfg); err != nil && runtime.GOOS == "windows" {
			return platform.MigrationResult{}, fmt.Errorf("preparar Windows Firewall/Hyper-V Firewall antes da migração: %w; execute o comando em um terminal como Administrador", err)
		}
	}
	paths := a.Store.Paths()
	_, unifiedStatErr := os.Stat(paths.Caddy)
	if unifiedStatErr != nil && !errors.Is(unifiedStatErr, os.ErrNotExist) {
		return platform.MigrationResult{}, fmt.Errorf("ler configuração Caddy WSL: %w", unifiedStatErr)
	}
	unifiedExisted := unifiedStatErr == nil
	preparedUnified := false
	cleanupPrepared := func() {
		if !preparedUnified {
			return
		}
		_ = a.Store.RollbackConfig()
		_ = a.Store.RollbackCaddy()
		_ = a.Store.RollbackPHPFiles()
	}
	if errors.Is(unifiedStatErr, os.ErrNotExist) {
		if _, applyErr := a.saveAndApplyMode(ctx, cfg, false, BootstrapTolerant); applyErr != nil {
			return platform.MigrationResult{}, fmt.Errorf("preparar configuração Caddy WSL: %w", applyErr)
		}
		preparedUnified = true
	}
	legacy := []string{}
	windowsLegacyExisted := false
	wslLegacyExisted := false
	for _, path := range []string{paths.WindowsCaddy, paths.WSLCaddy} {
		if _, statErr := os.Stat(path); statErr == nil {
			legacy = append(legacy, path)
			if path == paths.WindowsCaddy {
				windowsLegacyExisted = true
			} else if path == paths.WSLCaddy {
				wslLegacyExisted = true
			}
		}
	}
	backupRoot := filepath.Join(paths.Dir, "migration-backups", a.now().UTC().Format("20060102-150405.000000000"))
	caddyClient := a.edgeCaddy()
	initialUnifiedRunning := caddyClient.Status(ctx).Running
	unifiedStartAttempted := false
	unifiedStarted := false
	legacyStopAttempted := false
	legacyStopped := false
	legacyCaddy := a.WindowsCaddy
	if legacyCaddy.Runner == nil {
		if _, windowsConfigErr := os.Stat(paths.WindowsCaddy); windowsConfigErr == nil {
			// This adapter is created only inside the migration window. It is not
			// part of the normal M8 lifecycle, but lets an upgrade stop a still
			// running pre-M8 Caddy when its binary is still installed.
			legacyCaddy = platform.NewLocalCaddy("")
		}
	}
	wslConfigPath := a.WSLConfigPath
	if strings.TrimSpace(wslConfigPath) == "" {
		wslConfigPath = platform.UserWSLConfigPath()
	}
	oldWSLConfig, wslConfigErr := os.ReadFile(wslConfigPath)
	if wslConfigErr != nil && !errors.Is(wslConfigErr, os.ErrNotExist) {
		cleanupPrepared()
		return platform.MigrationResult{}, fmt.Errorf("ler .wslconfig: %w", wslConfigErr)
	}
	wslConfigExisted := wslConfigErr == nil
	if wslConfigExisted {
		if err := os.MkdirAll(backupRoot, 0o700); err != nil {
			cleanupPrepared()
			return platform.MigrationResult{}, fmt.Errorf("criar backup da configuração WSL: %w", err)
		}
		if err := os.WriteFile(filepath.Join(backupRoot, "wslconfig"), oldWSLConfig, 0o600); err != nil {
			cleanupPrepared()
			return platform.MigrationResult{}, fmt.Errorf("salvar backup da configuração WSL: %w", err)
		}
	}
	if os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		if _, updateErr := platform.UpdateWSLConfig(wslConfigPath, platform.DefaultWSLConfigSettings()); updateErr != nil {
			cleanupPrepared()
			return platform.MigrationResult{}, updateErr
		}
	}
	restoreWSLConfig := func() error {
		if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
			return nil
		}
		return platform.RestoreWSLConfig(wslConfigPath, oldWSLConfig, wslConfigExisted)
	}

	migration := platform.CaddyMigration{
		UnifiedConfig:   paths.Caddy,
		LegacyFiles:     legacy,
		BackupRoot:      backupRoot,
		ConfirmShutdown: confirmed,
		Now:             a.Now,
		ValidateUnified: func(ctx context.Context) error { return caddyClient.Validate(ctx, paths.Caddy) },
		// Mirrored networking puts the old Windows listeners and the new WSL
		// listeners on the same host namespace. The candidate is validated before
		// this handoff; rollback restarts the legacy edge if the new service does
		// not become healthy.
		StopLegacyBeforeStart: windowsLegacyExisted,
		StartUnified: func(ctx context.Context) error {
			unifiedStartAttempted = true
			status := caddyClient.Status(ctx)
			if caddyClient.RequireSystemd && !status.Systemd {
				// The host .wslconfig change takes effect only after the explicit
				// shutdown. The candidate was already validated; its service is
				// started by the second call after the VM comes back.
				return nil
			}
			if err := caddyClient.EnsureRunning(ctx, paths.Caddy); err != nil {
				return err
			}
			unifiedStarted = true
			return nil
		},
		HealthUnified: func(ctx context.Context) error {
			if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
				return nil
			}
			status := caddyClient.Status(ctx)
			if caddyClient.RequireSystemd && !status.Systemd {
				return nil
			}
			if status.Available && status.Running && status.Live {
				return nil
			}
			return fmt.Errorf("Caddy WSL único não está ativo: %s", status.Detail)
		},
		StopLegacy: func(ctx context.Context) error {
			if !windowsLegacyExisted || legacyCaddy.Runner == nil || !platform.IsAdminResponsive(platform.WindowsCaddyAdminAddress) {
				return nil
			}
			legacyStopAttempted = true
			if err := legacyCaddy.Stop(ctx); err != nil {
				return err
			}
			legacyStopped = true
			return nil
		},
		ShutdownWSL: func(ctx context.Context) error {
			if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
				return nil
			}
			return a.WSL.Shutdown(ctx)
		},
		VerifyAfterWSL: func(ctx context.Context) error {
			if os.Getenv("DEVLAN_TEST_MOCK") == "1" {
				return nil
			}
			report := a.WSLCompatibility(ctx)
			if !report.MirroredNetworking || !report.Systemd || !report.LoopbackBidirectional || !report.LANReachable || len(report.PortConflicts) > 0 {
				return fmt.Errorf("mirrored=%t systemd=%t loopback=%t lan=%t conflicts=%d", report.MirroredNetworking, report.Systemd, report.LoopbackBidirectional, report.LANReachable, len(report.PortConflicts))
			}
			return nil
		},
		RemoveLegacy: func() error {
			for _, path := range legacy {
				if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					return removeErr
				}
			}
			return nil
		},
		Rollback: func(ctx context.Context, backupDir string) error {
			var rollbackErr error
			recordRollbackErr := func(err error) {
				if err != nil && rollbackErr == nil {
					rollbackErr = err
				}
			}
			for _, path := range legacy {
				backupPath := filepath.Join(backupDir, filepath.Base(path))
				data, readErr := os.ReadFile(backupPath)
				if readErr != nil {
					recordRollbackErr(readErr)
					continue
				}
				recordRollbackErr(restoreManagedFile(path, data, 0o644))
			}
			recordRollbackErr(restoreWSLConfig())
			unifiedBackup := filepath.Join(backupDir, "unified.Caddyfile")
			if unifiedExisted {
				data, readErr := os.ReadFile(unifiedBackup)
				if readErr != nil {
					recordRollbackErr(readErr)
				} else {
					restoreErr := restoreManagedFile(paths.Caddy, data, 0o644)
					recordRollbackErr(restoreErr)
					if restoreErr == nil && (initialUnifiedRunning || unifiedStartAttempted) {
						recordRollbackErr(caddyClient.EnsureRunning(ctx, paths.Caddy))
					}
				}
			} else {
				if unifiedStartAttempted || unifiedStarted {
					recordRollbackErr(caddyClient.Stop(ctx))
				}
				recordRollbackErr(os.Remove(paths.Caddy))
				if errors.Is(rollbackErr, os.ErrNotExist) {
					rollbackErr = nil
				}
			}
			if windowsLegacyExisted && legacyCaddy.Runner != nil && (legacyStopped || legacyStopAttempted) {
				recordRollbackErr(legacyCaddy.EnsureRunning(ctx, paths.WindowsCaddy))
			}
			// An explicitly injected legacy WSL adapter is supported for upgrade
			// tests/rollback, but the production App has no second operational
			// client. Never synthesize one here.
			if wslLegacyExisted && a.Caddy.Runner != nil && a.WSLCaddy.Runner != nil {
				recordRollbackErr(a.WSLCaddy.EnsureRunning(ctx, paths.WSLCaddy))
			}
			return rollbackErr
		},
	}
	result, err := migration.Migrate(ctx)
	if err != nil {
		// A validation/start failure can occur before the migration coordinator
		// has a reversible process phase, but it must still not leave the host
		// .wslconfig changed.
		_ = restoreWSLConfig()
		if !unifiedExisted {
			_ = os.Remove(paths.Caddy)
		}
		if preparedUnified {
			cleanupPrepared()
		}
		a.recordTelemetry("topology.migrate", map[string]string{"component": "caddy_wsl", "result": "rolled_back"})
		return result, err
	}
	_ = a.Store.AppendSecurityAudit("CADDY_TOPOLOGY_MIGRATE", "topologia única WSL ativada")
	if recordErr := a.Store.RecordManagedState("windows.wslconfig"); recordErr != nil {
		return result, fmt.Errorf("registrar proveniência de .wslconfig após migração: %w", recordErr)
	}
	if fingerprintErr := a.refreshWSLManagedFingerprints(ctx, "wsl.systemd-config", "wsl.caddy-config"); fingerprintErr != nil {
		return result, fmt.Errorf("registrar proveniência dos arquivos WSL após migração: %w", fingerprintErr)
	}
	a.recordTelemetry("topology.migrate", map[string]string{"component": "caddy_wsl", "result": "ok"})
	return result, nil
}

// RepairM8 reconciles the non-destructive parts of the single-Caddy topology.
// It never calls wsl --shutdown: changing .wslconfig is transactional, but
// applying it to the running VM is intentionally left to the explicit
// migration flow because shutdown terminates every WSL distribution.
func (a *App) RepairM8(ctx context.Context) (ApplyResult, error) {
	result := ApplyResult{}
	a.topologyMu.Lock()
	defer a.topologyMu.Unlock()
	if err := a.ensureInstallationManifest(ctx); err != nil {
		return result, err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return result, err
	}
	if os.Getenv("DEVLAN_TEST_MOCK") != "1" {
		path := a.WSLConfigPath
		if path == "" {
			path = platform.UserWSLConfigPath()
		}
		update, err := platform.UpdateWSLConfig(path, platform.DefaultWSLConfigSettings())
		if err != nil {
			return result, err
		}
		if update.Changed {
			result.Warnings = append(result.Warnings, "o .wslconfig foi atualizado; reinicie o WSL pelo fluxo de migração para aplicar networkingMode=mirrored")
		}
		if recordErr := a.Store.RecordManagedState("windows.wslconfig"); recordErr != nil {
			result.Warnings = append(result.Warnings, "não foi possível registrar a proveniência de .wslconfig: "+recordErr.Error())
		}
		if fingerprintErr := a.refreshWSLManagedFingerprints(ctx, "wsl.systemd-config"); fingerprintErr != nil {
			result.Warnings = append(result.Warnings, "não foi possível registrar a fingerprint de /etc/wsl.conf: "+fingerprintErr.Error())
		}
	}
	if err := a.ensureFirewallSpec(ctx, cfg); err != nil && runtime.GOOS == "windows" {
		return result, fmt.Errorf("reconciliar Windows Firewall/Hyper-V Firewall: %w", err)
	}
	paths := a.Store.Paths()
	caddyStatus := a.CaddyStatus(ctx)
	caddyClient := a.edgeCaddy()
	if caddyClient.RequireSystemd && !caddyStatus.Systemd {
		applied, applyErr := a.saveAndApplyMode(ctx, cfg, false, BootstrapTolerant)
		if applyErr != nil {
			return result, applyErr
		}
		result.Warnings = append(result.Warnings, applied.Warnings...)
		result.Warnings = append(result.Warnings, "systemd do Caddy aguardando o reinício explícito do WSL")
		result.Revision = applied.Revision
	} else if _, err := os.Stat(paths.Caddy); errors.Is(err, os.ErrNotExist) {
		applied, applyErr := a.saveAndApplyMode(ctx, cfg, true, OperationalStrict)
		if applyErr != nil {
			return result, applyErr
		}
		result.Warnings = append(result.Warnings, applied.Warnings...)
		result.Revision = applied.Revision
	} else {
		reloaded, reloadErr := a.Reload(ctx)
		if reloadErr != nil {
			return result, reloadErr
		}
		result.Warnings = append(result.Warnings, reloaded.Warnings...)
		result.Revision = reloaded.Revision
	}
	result.Status = statusFor(result)
	_ = a.Store.AppendSecurityAudit("CADDY_TOPOLOGY_REPAIR", "componentes não destrutivos reconciliados")
	a.recordTelemetry("topology.repair", map[string]string{"component": "caddy_wsl", "result": "ok", "status": result.Status})
	return result, nil
}
