package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/dougkusanagi/dev-lan/internal/platform"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a

func (a *App) CAInfo(ctx context.Context) (map[string]string, error) {
	path := a.Store.Paths().CARootExport
	if _, err := os.Stat(path); err != nil {
		caddyClient := a.edgeCaddy()
		if caddyClient.WSL && caddyClient.Runner != nil {
			if exportErr := caddyClient.ExportRootCA(ctx, path); exportErr != nil {
				path = ""
			}
		} else {
			path = platform.FindCARootCertPath()
		}
	}
	details := platform.ReadCARootDetails(path)
	info := map[string]string{"path": path, "exists": strconv.FormatBool(details.Exists), "valid": strconv.FormatBool(details.Valid)}
	if details.Fingerprint != "" {
		info["fingerprint"] = details.Fingerprint
	}
	trusted := false
	if details.Valid && runtime.GOOS == "windows" {
		if value, trustErr := a.resourceUseCases().IsTrusted(ctx, path); trustErr == nil {
			trusted = value
		}
	}
	info["trusted"] = strconv.FormatBool(trusted)
	if details.NotAfter != "" {
		info["not_after"] = details.NotAfter
	}
	info["renewal_due"] = strconv.FormatBool(details.RenewalDue)
	if details.RemainingDays > 0 {
		info["remaining_days"] = strconv.Itoa(details.RemainingDays)
	}
	if details.Detail != "" {
		info["detail"] = details.Detail
	}
	return info, nil
}

func (a *App) ExportCA(ctx context.Context, targetPath string) (string, error) {
	src := a.Store.Paths().CARootExport
	if _, err := os.Stat(src); err != nil {
		caddyClient := a.edgeCaddy()
		if caddyClient.WSL && caddyClient.Runner != nil {
			if err := caddyClient.ExportRootCA(ctx, src); err != nil {
				return "", err
			}
		} else {
			src = platform.FindCARootCertPath()
		}
	}
	if src == "" {
		return "", fmt.Errorf("certificado raiz da CA do Caddy não encontrado no sistema")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("ler CA raiz (%s): %w", src, err)
	}
	if targetPath == "" {
		targetPath = a.Store.Paths().CARootExport
	}
	if err := platform.ValidateCARootPEM(data); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", fmt.Errorf("criar diretório para certificado %s: %w", targetPath, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(targetPath), ".devlan-ca-export-*.tmp")
	if err != nil {
		return "", err
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("gravar certificado temporário: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := platform.AtomicReplaceFile(temporaryName, targetPath); err != nil {
		return "", fmt.Errorf("publicar certificado em %s: %w", targetPath, err)
	}
	_ = a.Store.AppendSecurityAudit("CA_EXPORT", fmt.Sprintf("target=%s", targetPath))
	return targetPath, nil
}

func (a *App) RotateCA(ctx context.Context) (ApplyResult, error) {
	result := ApplyResult{}
	if err := a.Trust(ctx); err != nil {
		result.Warnings = append(result.Warnings, "não foi possível atualizar a confiança da CA: "+err.Error())
	}
	_ = a.Store.AppendSecurityAudit("CA_ROTATE", "rotação de CA solicitada")
	reloadResult, err := a.Reload(ctx)
	result.Warnings = append(result.Warnings, reloadResult.Warnings...)
	return result, err
}

func (a *App) SecurityAuditLogs(ctx context.Context, lines int) (string, error) {
	return a.Store.ReadSecurityAudit(lines)
}

func (a *App) recordTelemetry(name string, attributes map[string]string) {
	_ = a.Telemetry.Record(name, attributes)
}

// ExportConfig returns the portable configuration envelope. It deliberately
// excludes authentication hashes and expiring exposure state.
