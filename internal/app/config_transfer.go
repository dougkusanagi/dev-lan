package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/config"
	"github.com/dougkusanagi/dev-lan/internal/diagnostic"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a

func (a *App) ExportConfig() ([]byte, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	return config.MarshalExport(cfg)
}

// ImportConfig validates a portable configuration before applying it. The
// generated files are validated/reloaded first; the persisted state changes
// only after the new runtime configuration is accepted.
func (a *App) ImportConfig(ctx context.Context, data []byte) (ApplyResult, error) {
	cfg, err := config.UnmarshalExport(data)
	if err != nil {
		return ApplyResult{}, err
	}
	result, err := a.SaveConfigAndApply(ctx, cfg, true)
	if err == nil {
		_ = a.appendLog("configuração importada")
		_ = a.Store.AppendSecurityAudit("CONFIG_IMPORT", "configuração portátil importada sem credenciais")
	}
	return result, err
}

// DiagnosticBundle creates a support artifact from an explicit allowlist of
// managed files. Project contents, environment variables and credentials are
// never traversed or included.
func (a *App) DiagnosticBundle(ctx context.Context, targetPath string) (string, error) {
	now := a.now()
	if a.Now != nil {
		now = a.Now()
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return "", err
	}
	exported, err := config.MarshalExport(cfg)
	if err != nil {
		return "", err
	}
	checks, doctorErr := a.Doctor(ctx, "")
	if doctorErr != nil {
		checks = []Check{{Name: "doctor", Status: "WARN", Detail: doctorErr.Error()}}
	}
	doctorData, err := json.MarshalIndent(checks, "", "  ")
	if err != nil {
		return "", fmt.Errorf("serializar diagnóstico: %w", err)
	}
	wslStatsData, err := json.MarshalIndent(a.WSL.StatsSnapshot(), "", "  ")
	if err != nil {
		return "", fmt.Errorf("serializar inventário WSL: %w", err)
	}
	topologySnapshot := a.Topology(ctx)
	topologyData, err := json.MarshalIndent(topologySnapshot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("serializar topologia M8: %w", err)
	}
	firewallData, err := json.MarshalIndent(topologySnapshot.Firewall, "", "  ")
	if err != nil {
		return "", fmt.Errorf("serializar estado de firewall: %w", err)
	}

	entries := map[string][]byte{
		"config.json":   exported,
		"doctor.json":   append(doctorData, '\n'),
		"wsl.json":      append(wslStatsData, '\n'),
		"topology.json": append(topologyData, '\n'),
		"firewall.json": append(firewallData, '\n'),
		"runtime.txt":   []byte(fmt.Sprintf("runtime=%s\ndata_dir=%s\n", RuntimeDescription(), a.Store.Paths().Dir)),
	}
	paths := a.Store.Paths()
	for archiveName, sourcePath := range map[string]string{
		"generated/Caddyfile": paths.Caddy,
		"logs/devlan.log":     filepath.Join(paths.LogsDir, "devlan.log"),
		"logs/security.log":   paths.SecurityLog,
	} {
		data, readErr := os.ReadFile(sourcePath)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return "", fmt.Errorf("ler %s para o diagnóstico: %w", sourcePath, readErr)
		}
		if strings.HasSuffix(archiveName, "Caddyfile") {
			data = redactDiagnosticConfig(data)
		}
		entries[archiveName] = data
	}

	if targetPath == "" {
		stamp := now.UTC().Format("20060102-150405")
		targetPath = filepath.Join(paths.Dir, "devlan-diagnostic-"+stamp+".zip")
	}
	manifest := diagnostic.Manifest{
		Format:    diagnostic.Format,
		Version:   diagnostic.Version,
		CreatedAt: now.UTC().Format(time.RFC3339),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
	if err := diagnostic.Write(targetPath, manifest, entries); err != nil {
		return "", err
	}
	_ = a.appendLog(fmt.Sprintf("diagnóstico exportado: %s", targetPath))
	return targetPath, nil
}

func redactDiagnosticConfig(data []byte) []byte {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	inBasicAuth := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "basicauth {" {
			inBasicAuth = true
			continue
		}
		if inBasicAuth {
			if trimmed == "}" {
				inBasicAuth = false
				continue
			}
			lines[index] = "            <credencial removida do diagnóstico>"
		}
	}
	return []byte(strings.Join(lines, "\n"))
}
