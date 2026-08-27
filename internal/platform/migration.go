package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CaddyTopology string

const (
	TopologyUnknown   CaddyTopology = "unknown"
	TopologyDual      CaddyTopology = "dual-caddy"
	TopologySingleWSL CaddyTopology = "single-wsl"
	TopologyPartial   CaddyTopology = "partial"
)

type TopologySnapshot struct {
	Topology       CaddyTopology `json:"topology"`
	UnifiedConfig  bool          `json:"unifiedConfig"`
	WindowsConfig  bool          `json:"windowsConfig"`
	WSLConfig      bool          `json:"wslConfig"`
	WindowsRunning bool          `json:"windowsRunning"`
	WSLRunning     bool          `json:"wslRunning"`
}

func DetectCaddyTopology(unifiedConfig, windowsConfig, wslConfig, windowsRunning, wslRunning bool) TopologySnapshot {
	snapshot := TopologySnapshot{
		UnifiedConfig: unifiedConfig, WindowsConfig: windowsConfig, WSLConfig: wslConfig,
		WindowsRunning: windowsRunning, WSLRunning: wslRunning,
	}
	switch {
	case unifiedConfig && !windowsRunning && !windowsConfig && !wslConfig:
		snapshot.Topology = TopologySingleWSL
	case windowsConfig && wslConfig && (windowsRunning || wslRunning):
		snapshot.Topology = TopologyDual
	case unifiedConfig && (windowsConfig || wslConfig || windowsRunning):
		snapshot.Topology = TopologyPartial
	case unifiedConfig:
		snapshot.Topology = TopologySingleWSL
	case windowsConfig || wslConfig || windowsRunning || wslRunning:
		snapshot.Topology = TopologyPartial
	default:
		snapshot.Topology = TopologyUnknown
	}
	return snapshot
}

type MigrationStep string

const (
	MigrationBackup       MigrationStep = "backup"
	MigrationValidate     MigrationStep = "validate-unified"
	MigrationStartUnified MigrationStep = "start-unified"
	MigrationHealth       MigrationStep = "health-unified"
	MigrationStopLegacy   MigrationStep = "stop-legacy"
	MigrationShutdownWSL  MigrationStep = "shutdown-wsl"
	MigrationRemoveLegacy MigrationStep = "remove-legacy"
)

type MigrationResult struct {
	Topology          CaddyTopology   `json:"topology"`
	BackupDir         string          `json:"backupDir,omitempty"`
	UnifiedBackupPath string          `json:"unifiedBackupPath,omitempty"`
	Steps             []MigrationStep `json:"steps"`
	RolledBack        bool            `json:"rolledBack"`
}

// CaddyMigration is deliberately callback-based at the process boundary. It
// keeps the ordering and rollback guarantees testable while the App supplies
// the real Store/Caddy/systemd adapters.
type CaddyMigration struct {
	UnifiedConfig   string
	LegacyFiles     []string
	BackupRoot      string
	ConfirmShutdown bool
	Now             func() time.Time
	ValidateUnified func(context.Context) error
	// StopLegacyBeforeStart is used when the new edge must take over the same
	// mirrored host ports as the legacy edge. Validation still happens first;
	// once the candidate is known to be valid, the old process is stopped for
	// the short port handoff and is restored by Rollback on any failure.
	StopLegacyBeforeStart bool
	StartUnified          func(context.Context) error
	HealthUnified         func(context.Context) error
	StopLegacy            func(context.Context) error
	ShutdownWSL           func(context.Context) error
	VerifyAfterWSL        func(context.Context) error
	RemoveLegacy          func() error
	Rollback              func(context.Context, string) error
}

func (m CaddyMigration) timestamp() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m CaddyMigration) Migrate(ctx context.Context) (result MigrationResult, err error) {
	if !m.ConfirmShutdown {
		return result, ErrWSLShutdownConfirmation
	}
	if m.ValidateUnified == nil || m.StartUnified == nil || m.HealthUnified == nil {
		return result, errors.New("migração Caddy sem callbacks de validação/start/health")
	}
	result.Steps = []MigrationStep{}
	backupDir := m.BackupRoot
	if strings.TrimSpace(backupDir) == "" {
		backupDir = filepath.Join(os.TempDir(), "devlan-migration-"+m.timestamp().UTC().Format("20060102-150405.000000000"))
	}
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return result, fmt.Errorf("criar backup da topologia anterior: %w", err)
	}
	result.BackupDir = backupDir
	result.Steps = append(result.Steps, MigrationBackup)
	if strings.TrimSpace(m.UnifiedConfig) != "" {
		data, readErr := os.ReadFile(m.UnifiedConfig)
		if readErr == nil {
			result.UnifiedBackupPath = filepath.Join(backupDir, "unified.Caddyfile")
			if writeErr := os.WriteFile(result.UnifiedBackupPath, data, 0o600); writeErr != nil {
				return result, fmt.Errorf("salvar backup da configuração Caddy unificada: %w", writeErr)
			}
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return result, fmt.Errorf("ler configuração Caddy unificada: %w", readErr)
		}
	}
	for _, source := range m.LegacyFiles {
		if strings.TrimSpace(source) == "" {
			continue
		}
		data, readErr := os.ReadFile(source)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return result, fmt.Errorf("ler artefato legado %s: %w", source, readErr)
		}
		target := filepath.Join(backupDir, filepath.Base(source))
		if writeErr := os.WriteFile(target, data, 0o600); writeErr != nil {
			return result, fmt.Errorf("salvar backup de %s: %w", source, writeErr)
		}
	}

	result.Steps = append(result.Steps, MigrationValidate)
	if err := m.ValidateUnified(ctx); err != nil {
		return m.rollback(ctx, result, fmt.Errorf("validar Caddy WSL antes da migração: %w", err))
	}
	legacyStoppedBeforeStart := false
	if m.StopLegacyBeforeStart && m.StopLegacy != nil {
		result.Steps = append(result.Steps, MigrationStopLegacy)
		if err := m.StopLegacy(ctx); err != nil {
			return m.rollback(ctx, result, err)
		}
		legacyStoppedBeforeStart = true
	}
	result.Steps = append(result.Steps, MigrationStartUnified)
	if err := m.StartUnified(ctx); err != nil {
		return m.rollback(ctx, result, fmt.Errorf("iniciar Caddy WSL antes da migração: %w", err))
	}
	result.Steps = append(result.Steps, MigrationHealth)
	if err := m.HealthUnified(ctx); err != nil {
		return m.rollback(ctx, result, fmt.Errorf("healthcheck Caddy WSL antes da migração: %w", err))
	}
	if m.StopLegacy != nil && !legacyStoppedBeforeStart {
		result.Steps = append(result.Steps, MigrationStopLegacy)
		if err := m.StopLegacy(ctx); err != nil {
			return m.rollback(ctx, result, err)
		}
	}
	if m.ShutdownWSL != nil {
		result.Steps = append(result.Steps, MigrationShutdownWSL)
		if err := m.ShutdownWSL(ctx); err != nil {
			return m.rollback(ctx, result, err)
		}
		// The WSL shutdown terminates the new service too. It must be brought
		// back and checked before old artifacts are removed.
		if err := m.StartUnified(ctx); err != nil {
			return m.rollback(ctx, result, err)
		}
		if err := m.HealthUnified(ctx); err != nil {
			return m.rollback(ctx, result, err)
		}
		if m.VerifyAfterWSL != nil {
			if err := m.VerifyAfterWSL(ctx); err != nil {
				return m.rollback(ctx, result, fmt.Errorf("verificar modo efetivo após reiniciar o WSL: %w", err))
			}
		}
	}
	if m.RemoveLegacy != nil {
		result.Steps = append(result.Steps, MigrationRemoveLegacy)
		if err := m.RemoveLegacy(); err != nil {
			return m.rollback(ctx, result, err)
		}
	}
	result.Topology = TopologySingleWSL
	return result, nil
}

func (m CaddyMigration) rollback(ctx context.Context, result MigrationResult, cause error) (MigrationResult, error) {
	result.RolledBack = true
	result.Topology = TopologyPartial
	if m.Rollback != nil {
		// Recovery must not inherit an expired migration deadline. Preserve any
		// context values used for attribution, but give rollback its own bounded
		// window so a timeout in the forward path cannot make recovery fail by
		// construction.
		if ctx == nil {
			ctx = context.Background()
		}
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		defer cancel()
		if rollbackErr := m.Rollback(rollbackCtx, result.BackupDir); rollbackErr != nil {
			return result, fmt.Errorf("%w; rollback também falhou: %v", cause, rollbackErr)
		}
	}
	return result, fmt.Errorf("%w; topologia anterior preservada em %s", cause, result.BackupDir)
}
