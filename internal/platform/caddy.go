package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// UnifiedCaddyAdminAddress is reachable only from loopback and belongs to
	// the single systemd-managed Caddy in WSL.
	UnifiedCaddyAdminAddress = "127.0.0.1:2020"
	WSLCaddyAdminAddress     = UnifiedCaddyAdminAddress
	// WindowsCaddyAdminAddress is retained only so older callers can report a
	// migration candidate. No M8 runtime path starts or reloads a Windows Caddy.
	WindowsCaddyAdminAddress = "127.0.0.1:2019"
)

type CaddyClient struct {
	Runner         Runner
	WSL            bool
	Binary         string
	RequireSystemd bool
	ServiceName    string
	LiveConfigPath string
	// AdminProbe is injectable for deterministic tests. Production uses the
	// loopback admin HTTP endpoint and checks an actual 2xx response instead of
	// treating an open TCP socket as a healthy configuration.
	AdminProbe func(context.Context, string) bool
}

func NewLocalCaddy(binary string) CaddyClient {
	if binary == "" {
		binary = FindLocalCaddy()
	}
	if binary == "" {
		binary = "caddy"
	}
	return CaddyClient{Runner: NewExecRunner(binary), Binary: binary}
}

func NewWSLCaddy(runner WSLRunner) CaddyClient {
	return CaddyClient{Runner: runner, WSL: true, RequireSystemd: true, ServiceName: "caddy", LiveConfigPath: "/etc/caddy/Caddyfile"}
}

func (c CaddyClient) Available(ctx context.Context) error {
	if c.Runner == nil {
		return fmt.Errorf("%w: Caddy não configurado", ErrUnavailable)
	}
	var err error
	if c.WSL {
		_, err = c.Runner.Run(ctx, "caddy", "version")
	} else {
		// Local runners already contain the executable and expect only version.
		_, err = c.Runner.Run(ctx, "version")
	}
	return err
}

func (c CaddyClient) Validate(ctx context.Context, configPath string) error {
	if c.Runner == nil {
		return fmt.Errorf("%w: Caddy não configurado", ErrUnavailable)
	}
	if c.WSL {
		wslPath, err := ToWSLPath(configPath)
		if err != nil {
			return err
		}
		_, err = c.Runner.Run(ctx, "caddy", "validate", "--config", wslPath, "--adapter", "caddyfile")
		return err
	}
	_, err := c.Runner.Run(ctx, "validate", "--config", configPath, "--adapter", "caddyfile")
	return err
}

func (c CaddyClient) Reload(ctx context.Context, configPath string) error {
	if c.Runner == nil {
		return fmt.Errorf("%w: Caddy não configurado", ErrUnavailable)
	}
	if c.WSL {
		if c.RequireSystemd {
			// The systemd client owns the live service file. Route public reloads
			// through the same validate/publish/rollback transaction as EnsureRunning
			// instead of replacing /etc/caddy/Caddyfile in place.
			return c.ensureSystemdRunning(ctx, configPath)
		}
		wslPath, err := ToWSLPath(configPath)
		if err != nil {
			return err
		}
		_, err = c.Runner.Run(ctx, "caddy", "reload", "--address", WSLCaddyAdminAddress, "--config", wslPath, "--adapter", "caddyfile")
		return err
	}
	_, err := c.Runner.Run(ctx, "reload", "--address", WindowsCaddyAdminAddress, "--config", configPath, "--adapter", "caddyfile")
	return err
}

// Trust installs Caddy's local root CA in the current Windows trust store.
// Caddy may require an elevated terminal depending on host policy.
func (c CaddyClient) Trust(ctx context.Context) error {
	if c.Runner == nil {
		return fmt.Errorf("%w: Caddy não configurado", ErrUnavailable)
	}
	if c.WSL {
		_, err := c.runWSL(ctx, false, "caddy", "trust")
		return err
	}
	_, err := c.Runner.Run(ctx, "trust")
	return err
}

func (c CaddyClient) HashPassword(ctx context.Context, password string) (string, error) {
	if c.Runner == nil {
		return "", fmt.Errorf("%w: Caddy não configurado", ErrUnavailable)
	}
	var out string
	var err error
	if c.WSL {
		out, err = c.Runner.Run(ctx, "caddy", "hash-password", "--plaintext", password)
	} else {
		out, err = c.Runner.Run(ctx, "hash-password", "--plaintext", password)
	}
	if err != nil {
		return "", fmt.Errorf("gerar hash de senha pelo Caddy: %w", err)
	}
	return out, nil
}

// EnsureRunning reloads a running Caddy or starts the local Windows instance
// after a reboot or a previously interrupted installation.
func (c CaddyClient) EnsureRunning(ctx context.Context, configPath string) error {
	if c.WSL {
		if c.RequireSystemd {
			return c.ensureSystemdRunning(ctx, configPath)
		}
		if err := c.Reload(ctx, configPath); err == nil {
			return nil
		}
		wslPath, err := ToWSLPath(configPath)
		if err != nil {
			return err
		}
		// The package normally installs Caddy as a service, but WSL services can
		// be unavailable after a reboot or when systemd is disabled. Start a
		// detached fallback so the GUI can recover without a terminal.
		script := fmt.Sprintf("nohup caddy run --config %s --adapter caddyfile >/tmp/devlan-caddy.log 2>&1 </dev/null &", shellQuote(wslPath))
		if _, err := c.Runner.Run(ctx, "/bin/sh", "-c", script); err != nil {
			return fmt.Errorf("iniciar Caddy no WSL: %w", err)
		}

		var lastErr error
		for attempt := 0; attempt < 20; attempt++ {
			if err := c.Reload(ctx, configPath); err == nil {
				return nil
			} else {
				lastErr = err
			}
			time.Sleep(250 * time.Millisecond)
		}
		return fmt.Errorf("Caddy no WSL não respondeu após iniciar: %w", lastErr)
	}
	reloadErr := c.Reload(ctx, configPath)
	if reloadErr == nil {
		return nil
	}
	// An active admin endpoint means this is a real configuration reload
	// failure (for example a listener conflict), not an absent process. Starting
	// a second Caddy would only return a successful spawn while that child exits
	// asynchronously, causing DevLAN to report a false-positive reload.
	if IsAdminResponsive(WindowsCaddyAdminAddress) {
		return reloadErr
	}
	if c.Binary == "" {
		return fmt.Errorf("%w: Caddy Windows não configurado", ErrUnavailable)
	}
	// `caddy start` waits for a pingback child process on Windows and can hold
	// the caller indefinitely. Start `run` detached with discarded output;
	// future operations use the admin API through Reload.
	command := exec.Command(c.Binary, "run", "--config", configPath, "--adapter", "caddyfile")
	command.Stdout = nil
	command.Stderr = nil
	hideProcessWindow(command)
	if err := command.Start(); err != nil {
		return fmt.Errorf("iniciar Caddy Windows: %w", err)
	}
	_ = command.Process.Release()
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		if err := c.Reload(ctx, configPath); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("Caddy Windows não respondeu após iniciar: %w", lastErr)
}

// CaddyServiceStatus is a live, non-persisted snapshot used by doctor/status.
// It distinguishes binary availability, systemd lifecycle and the admin
// endpoint so a partial WSL installation is actionable.
type CaddyServiceStatus struct {
	Available    bool   `json:"available"`
	Running      bool   `json:"running"`
	Systemd      bool   `json:"systemd"`
	Live         bool   `json:"live"`
	AdminAddress string `json:"adminAddress"`
	ConfigPath   string `json:"configPath,omitempty"`
	Detail       string `json:"detail,omitempty"`
}

func (c CaddyClient) serviceName() string {
	if strings.TrimSpace(c.ServiceName) != "" {
		return strings.TrimSpace(c.ServiceName)
	}
	return "caddy"
}

func (c CaddyClient) liveConfigPath() string {
	if strings.TrimSpace(c.LiveConfigPath) != "" {
		return strings.TrimSpace(c.LiveConfigPath)
	}
	return "/etc/caddy/Caddyfile"
}

type wslRootRunner interface {
	RunAsRootOperation(context.Context, string, ...string) (string, error)
}

func (c CaddyClient) runWSL(ctx context.Context, root bool, args ...string) (string, error) {
	if c.Runner == nil {
		return "", fmt.Errorf("%w: Caddy no WSL não configurado", ErrUnavailable)
	}
	if root {
		if runner, ok := c.Runner.(wslRootRunner); ok {
			return runner.RunAsRootOperation(ctx, WSLOperationReload, args...)
		}
	}
	return c.Runner.Run(ctx, args...)
}

func (c CaddyClient) copyConfigToService(ctx context.Context, configPath string) error {
	source, err := ToWSLPath(configPath)
	if err != nil {
		return err
	}
	target := c.liveConfigPath()
	if _, err := c.runWSL(ctx, true, "/bin/mkdir", "-p", pathpkg.Dir(target)); err != nil {
		return fmt.Errorf("preparar diretório da configuração Caddy: %w", err)
	}
	temporary := target + ".devlan.tmp"
	defer func() { _, _ = c.runWSL(ctx, true, "/bin/rm", "-f", "--", temporary) }()
	if _, err := c.runWSL(ctx, true, "/bin/cp", "--", source, temporary); err != nil {
		return fmt.Errorf("publicar configuração Caddy no WSL: %w", err)
	}
	if _, err := c.runWSL(ctx, true, "/bin/chmod", "0644", temporary); err != nil {
		return fmt.Errorf("ajustar permissões da configuração Caddy: %w", err)
	}
	if _, err := c.runWSL(ctx, true, "/bin/mv", "-f", "--", temporary, target); err != nil {
		return fmt.Errorf("publicar configuração Caddy atomicamente: %w", err)
	}
	return nil
}

type liveConfigSnapshot struct {
	backupPath string
	exists     bool
}

// snapshotLiveConfig keeps the last service configuration inside the WSL
// service directory. It is intentionally made by fixed commands with path
// arguments, so neither the managed source nor a user-supplied filename is
// interpolated into shell code.
func (c CaddyClient) snapshotLiveConfig(ctx context.Context) (liveConfigSnapshot, error) {
	target := c.liveConfigPath()
	backup := target + ".devlan.previous"
	const script = `set -e
if [ -f "$1" ]; then
    /bin/cp -- "$1" "$2"
    printf 'present'
else
    /bin/rm -f -- "$2"
    printf 'absent'
fi`
	out, err := c.runWSL(ctx, true, "/bin/sh", "-c", script, "devlan-live", target, backup)
	if err != nil {
		return liveConfigSnapshot{}, fmt.Errorf("salvar configuração viva anterior do Caddy: %w", err)
	}
	return liveConfigSnapshot{backupPath: backup, exists: strings.EqualFold(strings.TrimSpace(out), "present")}, nil
}

func (c CaddyClient) restoreLiveConfig(ctx context.Context, snapshot liveConfigSnapshot) error {
	if snapshot.backupPath == "" {
		return nil
	}
	target := c.liveConfigPath()
	if snapshot.exists {
		temporary := target + ".devlan.rollback"
		const script = `set -e
/bin/mkdir -p -- "$1"
/bin/cp -- "$2" "$3"
/bin/chmod 0644 "$3"
/bin/mv -f -- "$3" "$4"`
		if _, err := c.runWSL(ctx, true, "/bin/sh", "-c", script, "devlan-rollback", pathpkg.Dir(target), snapshot.backupPath, temporary, target); err != nil {
			return fmt.Errorf("restaurar configuração viva anterior do Caddy: %w", err)
		}
	} else if _, err := c.runWSL(ctx, true, "/bin/rm", "-f", "--", target); err != nil {
		return fmt.Errorf("remover configuração viva parcial do Caddy: %w", err)
	}
	return nil
}

func (c CaddyClient) discardLiveConfigSnapshot(ctx context.Context, snapshot liveConfigSnapshot) {
	if snapshot.backupPath != "" {
		_, _ = c.runWSL(ctx, true, "/bin/rm", "-f", "--", snapshot.backupPath)
	}
}

// publishAndReload publishes a validated candidate and restores the previous
// service file if any lifecycle or health step fails. Store rollback alone is
// insufficient because the systemd service may already have consumed the new
// file before the process healthcheck reports a failure.
func (c CaddyClient) publishAndReload(ctx context.Context, configPath string, apply func() error) error {
	snapshot, err := c.snapshotLiveConfig(ctx)
	if err != nil {
		return err
	}
	if err := c.copyConfigToService(ctx, configPath); err != nil {
		c.discardLiveConfigSnapshot(ctx, snapshot)
		return err
	}
	if err := apply(); err != nil {
		rollbackErr := c.restoreLiveConfig(ctx, snapshot)
		if rollbackErr == nil && c.systemdActive(ctx) {
			rollbackErr = c.reloadLive(ctx)
		}
		c.discardLiveConfigSnapshot(ctx, snapshot)
		if rollbackErr != nil {
			return fmt.Errorf("%w; rollback da configuração viva falhou: %v", err, rollbackErr)
		}
		return err
	}
	c.discardLiveConfigSnapshot(ctx, snapshot)
	return nil
}

func (c CaddyClient) systemd(ctx context.Context, action string) error {
	_, err := c.runWSL(ctx, true, "/usr/bin/systemctl", action, c.serviceName())
	return err
}

func (c CaddyClient) systemdActive(ctx context.Context) bool {
	_, err := c.runWSL(ctx, false, "/usr/bin/systemctl", "is-active", "--quiet", c.serviceName())
	return err == nil
}

func (c CaddyClient) ensureSystemdRunning(ctx context.Context, configPath string) error {
	if err := c.Validate(ctx, configPath); err != nil {
		return fmt.Errorf("validar Caddy WSL: %w", err)
	}
	return c.publishAndReload(ctx, configPath, func() error {
		if !c.systemdActive(ctx) {
			if err := c.systemd(ctx, "start"); err != nil {
				return fmt.Errorf("iniciar serviço systemd do Caddy: %w", err)
			}
		} else if err := c.systemd(ctx, "reload"); err != nil {
			return fmt.Errorf("recarregar serviço systemd do Caddy: %w", err)
		}
		if err := c.reloadLive(ctx); err != nil {
			return err
		}
		if !c.systemdActive(ctx) {
			return fmt.Errorf("healthcheck Caddy WSL falhou: serviço systemd não está ativo")
		}
		if !c.adminLive(ctx) {
			return fmt.Errorf("healthcheck Caddy WSL falhou: endpoint admin não respondeu")
		}
		return nil
	})
}

func (c CaddyClient) reloadLive(ctx context.Context) error {
	if c.Runner == nil {
		return fmt.Errorf("%w: Caddy não configurado", ErrUnavailable)
	}
	_, err := c.runWSL(ctx, false, "caddy", "reload", "--address", UnifiedCaddyAdminAddress, "--config", c.liveConfigPath(), "--adapter", "caddyfile")
	if err != nil {
		return fmt.Errorf("recarregar configuração viva do Caddy: %w", err)
	}
	return nil
}

func (c CaddyClient) Start(ctx context.Context, configPath string) error {
	if !c.WSL {
		return c.EnsureRunning(ctx, configPath)
	}
	if c.RequireSystemd {
		if err := c.Validate(ctx, configPath); err != nil {
			return err
		}
		return c.publishAndReload(ctx, configPath, func() error {
			if err := c.systemd(ctx, "start"); err != nil {
				return err
			}
			if err := c.reloadLive(ctx); err != nil {
				return err
			}
			if !c.systemdActive(ctx) || !c.adminLive(ctx) {
				return errors.New("healthcheck Caddy WSL falhou após start")
			}
			return nil
		})
	}
	return c.EnsureRunning(ctx, configPath)
}

// Restart replaces the live systemd instance only after the candidate has
// passed validation. It is used by repair/migration and is intentionally
// separate from EnsureRunning, which prefers a graceful reload.
func (c CaddyClient) Restart(ctx context.Context, configPath string) error {
	if !c.WSL || !c.RequireSystemd {
		return c.EnsureRunning(ctx, configPath)
	}
	if err := c.Validate(ctx, configPath); err != nil {
		return fmt.Errorf("validar Caddy WSL: %w", err)
	}
	return c.publishAndReload(ctx, configPath, func() error {
		if err := c.systemd(ctx, "restart"); err != nil {
			return fmt.Errorf("reiniciar serviço systemd do Caddy: %w", err)
		}
		if err := c.reloadLive(ctx); err != nil {
			return err
		}
		if !c.systemdActive(ctx) || !c.adminLive(ctx) {
			return errors.New("healthcheck Caddy WSL falhou após restart")
		}
		return nil
	})
}

func (c CaddyClient) adminAddress() string {
	if c.WSL || c.RequireSystemd {
		return UnifiedCaddyAdminAddress
	}
	return WindowsCaddyAdminAddress
}

func (c CaddyClient) Stop(ctx context.Context) error {
	if c.Runner == nil {
		return fmt.Errorf("%w: Caddy não configurado", ErrUnavailable)
	}
	if c.WSL && c.RequireSystemd {
		return c.systemd(ctx, "stop")
	}
	if c.WSL {
		_, err := c.runWSL(ctx, false, "caddy", "stop", "--address", UnifiedCaddyAdminAddress)
		return err
	}
	_, err := c.Runner.Run(ctx, "stop", "--address", WindowsCaddyAdminAddress)
	return err
}

func (c CaddyClient) Status(ctx context.Context) CaddyServiceStatus {
	status := CaddyServiceStatus{AdminAddress: c.adminAddress(), ConfigPath: c.liveConfigPath()}
	if c.Runner == nil {
		status.Detail = "Caddy não configurado"
		return status
	}
	if err := c.Available(ctx); err != nil {
		status.Detail = err.Error()
		return status
	}
	status.Available = true
	if c.WSL && c.RequireSystemd {
		status.Systemd = c.systemdAvailable(ctx)
		status.Running = status.Systemd && c.systemdActive(ctx)
	} else {
		status.Running = IsAdminResponsive(status.AdminAddress)
	}
	status.Live = c.adminLive(ctx)
	// A responsive admin socket is not enough for the canonical WSL edge: a
	// stray manually-started Caddy must not be reported as the systemd service.
	// For the compatibility/local client there is no systemd authority, so the
	// live endpoint remains the running signal.
	if !status.Running && status.Live && !(c.WSL && c.RequireSystemd) {
		status.Running = true
	}
	if !status.Running {
		if c.WSL && c.RequireSystemd && status.Systemd {
			status.Detail = "Caddy systemd ativo, mas o endpoint admin/config não respondeu"
		} else {
			status.Detail = "Caddy disponível, mas o serviço não está ativo"
		}
	}
	return status
}

func (c CaddyClient) adminLive(ctx context.Context) bool {
	if c.AdminProbe != nil {
		return c.AdminProbe(ctx, c.adminAddress())
	}
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+c.adminAddress()+"/config/", nil)
	if err != nil {
		return false
	}
	response, err := (&http.Client{Timeout: 750 * time.Millisecond}).Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices
}

// AdminLive performs the host-side loopback half of the WSL compatibility
// probe. It is public so the application can combine it with the reverse
// WSL-to-Windows probe without exposing the Caddy HTTP client itself.
func (c CaddyClient) AdminLive(ctx context.Context) bool {
	return c.adminLive(ctx)
}

func (c CaddyClient) systemdAvailable(ctx context.Context) bool {
	_, err := c.runWSL(ctx, false, "/bin/sh", "-c", "[ \"$(ps -p 1 -o comm= 2>/dev/null | tr -d ' ')\" = systemd ]")
	return err == nil
}

// ExportRootCA reads only Caddy's public root certificate from the WSL service
// data directory. The known candidate paths are fixed and the output is
// checked as a CA certificate before it is published on the Windows side.
func (c CaddyClient) ExportRootCA(ctx context.Context, targetPath string) error {
	if !c.WSL {
		return fmt.Errorf("exportação da CA deve usar o Caddy WSL")
	}
	if strings.TrimSpace(targetPath) == "" {
		return errors.New("destino da CA raiz vazio")
	}
	var data string
	var lastErr error
	for _, candidate := range []string{
		"/var/lib/caddy/.local/share/caddy/pki/authorities/local/root.crt",
		"/var/lib/caddy/.config/caddy/pki/authorities/local/root.crt",
		"/root/.local/share/caddy/pki/authorities/local/root.crt",
	} {
		// Debian's systemd service stores the public root under /var/lib/caddy
		// and normally restricts traversal to the caddy service account. Read
		// the certificate as root, but copy only this fixed public file.
		data, lastErr = c.runWSL(ctx, true, "/bin/cat", candidate)
		if lastErr == nil && strings.Contains(data, "BEGIN CERTIFICATE") {
			break
		}
	}
	if !strings.Contains(data, "BEGIN CERTIFICATE") {
		if lastErr == nil {
			lastErr = errors.New("conteúdo não contém certificado PEM")
		}
		return fmt.Errorf("ler certificado raiz do Caddy WSL: %w", lastErr)
	}
	if err := ValidateCARootPEM([]byte(data)); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(targetPath), ".devlan-ca-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFileAtomic(temporaryName, targetPath)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// FindLocalCaddy resolves both a PATH installation and Caddy installed by
// WinGet, whose package directory is not always present in the current PATH.
func FindLocalCaddy() string {
	if path, err := exec.LookPath("caddy.exe"); err == nil {
		return path
	}
	if path, err := exec.LookPath("caddy"); err == nil {
		return path
	}
	if runtime.GOOS != "windows" {
		return ""
	}
	var candidates []string
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		matches, _ := filepath.Glob(filepath.Join(localAppData, "Microsoft", "WinGet", "Packages", "CaddyServer.Caddy_*", "caddy.exe"))
		candidates = append(candidates, matches...)
	}
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		candidates = append(candidates, filepath.Join(programFiles, "Caddy", "caddy.exe"))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
