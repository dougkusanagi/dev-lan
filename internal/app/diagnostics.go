package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

// ErrPasswordHashUnavailable is returned before any configuration write when
// Caddy cannot produce a password hash. A runtime failure must never cause a

func (a *App) URL(ctx context.Context, projectName string) (string, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return "", err
	}
	resolved, err := cfg.Resolve(projectName)
	if err != nil {
		return "", err
	}
	host := cfg.LANAddress
	if resolvedHost, resolveErr := a.resourceUseCases().LANAddress(ctx, host); resolveErr == nil {
		host = resolvedHost
	} else if host == "auto" || strings.TrimSpace(host) == "" {
		host = "localhost"
	}
	return resolved.URL(host, cfg.WindowsPort, cfg.HTTPSPort, cfg.SecureProject(resolved.Project)), nil
}

func (a *App) URLs(ctx context.Context) ([]string, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	host := cfg.LANAddress
	if resolvedHost, resolveErr := a.resourceUseCases().LANAddress(ctx, host); resolveErr == nil {
		host = resolvedHost
	} else if host == "auto" || strings.TrimSpace(host) == "" {
		host = "localhost"
	}
	effective, err := a.EffectiveConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	resolved, err := effective.ResolvedProjects()
	if err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(resolved))
	for _, item := range resolved {
		urls = append(urls, item.URL(host, cfg.WindowsPort, cfg.HTTPSPort, effective.SecureProject(item.Project)))
	}
	return urls, nil
}

func (a *App) Open(ctx context.Context, projectName string) (string, error) {
	url, err := a.URL(ctx, projectName)
	if err != nil {
		return "", err
	}
	if err := platform.OpenURL(url); err != nil {
		return url, err
	}
	return url, nil
}

func (a *App) Logs(component string) (string, error) {
	paths := a.Store.Paths()
	if component == "" || component == "devlan" {
		data, err := os.ReadFile(filepath.Join(paths.LogsDir, "devlan.log"))
		if errors.Is(err, os.ErrNotExist) {
			return "(nenhum log ainda)\n", nil
		}
		return string(data), err
	}
	if strings.ContainsAny(component, `/\\`) || component == "." || component == ".." {
		return "", fmt.Errorf("componente de log inválido")
	}
	if strings.HasPrefix(component, "php-") {
		version := strings.TrimPrefix(component, "php-")
		if manager, ok := a.PHP.(platform.PHPInfoManager); ok {
			return manager.Logs(context.Background(), version)
		}
	}
	data, err := os.ReadFile(filepath.Join(paths.LogsDir, component+".log"))
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("log não encontrado: %s", component)
	}
	return string(data), err
}

func (a *App) appendLog(message string) error {
	if err := a.Store.Ensure(); err != nil {
		return err
	}
	stamp := a.now().Format(time.RFC3339)
	line := fmt.Sprintf("%s %s\n", stamp, message)
	file, err := os.OpenFile(filepath.Join(a.Store.Paths().LogsDir, "devlan.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(line)
	return err
}

func RuntimeDescription() string {
	return application.RuntimeDescription()
}

func extractCaddyLANAddress(caddyfilePath string) string {
	data, err := os.ReadFile(caddyfilePath)
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`default_sni\s+([^\s\r\n]+)`)
	matches := re.FindStringSubmatch(string(data))
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (a *App) CheckLANAddressDivergence() (current string, generated string, diverged bool) {
	cfg, err := a.Store.Load()
	if err != nil || cfg.LANAddress != "auto" {
		return "", "", false
	}
	current, err = a.resourceUseCases().LANAddress(context.Background(), cfg.LANAddress)
	if err != nil {
		return "", "", false
	}
	generated = extractCaddyLANAddress(a.Store.Paths().Caddy)
	if generated == "" {
		generated = extractCaddyLANAddress(a.Store.Paths().WindowsCaddy)
	}
	if generated != "" && generated != "localhost" && generated != "127.0.0.1" && current != generated {
		return current, generated, true
	}
	return current, generated, false
}

// CaddyTopologyStatus returns the live topology without treating persisted
// files as proof that a process is healthy.
func (a *App) CaddyTopologyStatus(ctx context.Context) platform.TopologySnapshot {
	paths := a.Store.Paths()
	_, unifiedErr := os.Stat(paths.Caddy)
	_, windowsErr := os.Stat(paths.WindowsCaddy)
	_, wslErr := os.Stat(paths.WSLCaddy)
	// Status must not make the legacy Windows admin endpoint part of the normal
	// health graph. Its artifact is enough to identify a partial migration;
	// only the explicit migration coordinator may probe/stop that old process.
	windowsRunning := false
	edge := a.edgeCaddy()
	edgeStatus := edge.Status(ctx)
	return platform.DetectCaddyTopology(unifiedErr == nil, windowsErr == nil, wslErr == nil, windowsRunning, edgeStatus.Running)
}

func restoreManagedFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".devlan-restore-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return platform.AtomicReplaceFile(temporaryName, path)
}

func (a *App) CaddyStatus(ctx context.Context) platform.CaddyServiceStatus {
	return a.edgeCaddy().Status(ctx)
}

func (a *App) WSLCompatibility(ctx context.Context) platform.WSLCompatibilityReport {
	cfg, err := a.Store.Load()
	if err != nil {
		return platform.WSLCompatibilityReport{Checks: []platform.CompatibilityCheck{{Name: "Configuração", Status: platform.CompatibilityFail, Detail: err.Error()}}}
	}
	ports := []int{80, 443}
	base, count := cfg.RouteBasePort, cfg.RoutePortCount
	if base == 0 {
		base = 8080
	}
	if count == 0 {
		count = 100
	}
	for port := base; port < base+count && port <= 65535; port++ {
		ports = append(ports, port)
	}
	effective, effectiveErr := a.EffectiveConfig(ctx, cfg)
	// Only listeners represented by the effective routing table are owned by
	// the live Caddy. An unrelated process using an unassigned port in the
	// configured pool is still a real allocation conflict and must not be
	// hidden merely because Caddy itself is healthy.
	managedPorts := map[int]bool{80: true, 443: true}
	if effectiveErr == nil {
		for _, project := range effective.Projects {
			if resolved, resolveErr := effective.Resolve(project.Name); resolveErr == nil {
				managedPorts[resolved.RoutePort] = true
			}
		}
	}
	for _, port := range cfg.RoutePortAllocations {
		managedPorts[port] = true
	}
	edgeStatus := a.edgeCaddy().Status(ctx)
	edgeLive := edgeStatus.Running && edgeStatus.Live
	lanHost := cfg.LANAddress
	if lanHost == "auto" || strings.TrimSpace(lanHost) == "" {
		lanHost, err = a.resourceUseCases().LANAddress(ctx, lanHost)
		if err != nil {
			lanHost = ""
		}
	}
	lanPort := 80
	if effectiveErr == nil && len(effective.Projects) > 0 {
		if resolved, resolveErr := effective.Resolve(effective.Projects[0].Name); resolveErr == nil && resolved.RoutePort > 0 {
			lanPort = resolved.RoutePort
		}
	}
	probe := platform.WSLCompatibilityProbe{
		WSL: a.WSL,
		WSLVersion: func() platform.Runner {
			if a.WSL.Invoker != nil {
				return a.WSL.Invoker
			}
			binary := a.WSL.Binary
			if binary == "" {
				binary = "wsl.exe"
			}
			return platform.NewExecRunner(binary)
		}(),
		ConfigText: func() string {
			path := a.WSLConfigPath
			if path == "" {
				path = platform.UserWSLConfigPath()
			}
			data, _ := os.ReadFile(path)
			return string(data)
		}(),
		PortAvailable: func(_ context.Context, port int) bool {
			if edgeLive && managedPorts[port] {
				return true
			}
			return platform.IsPortAvailable(port)
		},
		LANProbe: func(probeContext context.Context) error {
			if strings.TrimSpace(lanHost) == "" || lanHost == "localhost" || lanHost == "127.0.0.1" || lanHost == "::1" {
				return errors.New("endereço LAN não resolvido")
			}
			// Once the unified edge is prepared, probe the host's physical LAN
			// address from Windows. In mirrored mode this is the same inbound
			// listener a second machine uses; probing that address from inside WSL
			// is not reliable for a host's own interface on every WSL release.
			if _, unifiedErr := os.Stat(a.Store.Paths().Caddy); unifiedErr == nil {
				if probeErr := probeLANTCP(probeContext, lanHost, lanPort); probeErr != nil {
					// Some mirrored WSL builds do not hairpin a Windows
					// connection to the host's own LAN address. Verify the same
					// non-loopback listener from the mirrored Linux interface;
					// host and Hyper-V firewall policy is reconciled separately.
					if wslProbeErr := probeWSLLANTCP(probeContext, a.WSL, lanHost, lanPort); wslProbeErr != nil {
						return fmt.Errorf("probe LAN %s:%d falhou no Windows (%v) e no WSL (%w)", lanHost, lanPort, probeErr, wslProbeErr)
					}
				}
				return nil
			}
			// The probe originates inside the selected WSL distribution and
			// reaches the host's LAN address. It verifies the mirrored path with
			// the same direct listener that a second machine would use, without
			// interpolating the address into shell source.
			const script = `set -e
if command -v curl >/dev/null 2>&1; then
    curl --silent --show-error --connect-timeout 1 --max-time 2 -o /dev/null "http://$1:$2/"
elif command -v wget >/dev/null 2>&1; then
    wget -q -T 2 -O /dev/null "http://$1:$2/"
else
    exit 127
fi`
			_, probeErr := a.WSL.RunOperation(probeContext, platform.WSLOperationDoctor, "/bin/sh", "-c", script, "devlan", lanHost, strconv.Itoa(lanPort))
			if probeErr != nil {
				return fmt.Errorf("probe WSL→LAN %s:%d: %w", lanHost, lanPort, probeErr)
			}
			return nil
		},
		LoopbackProbe: func(probeContext context.Context) error {
			if !a.edgeCaddy().AdminLive(probeContext) {
				return errors.New("probe Windows→WSL no admin do Caddy não respondeu")
			}
			return nil
		},
		WSLToWindowsProbe: func(probeContext context.Context) error {
			return probeWSLToWindowsLoopback(probeContext, a.WSL)
		},
	}
	return probe.Check(ctx, a.WSL.Distribution, ports...)
}

// probeWSLToWindowsLoopback verifies mirrored localhost forwarding without
// depending on the long-running API service. The listener is private,
// ephemeral and exists only for the duration of this probe.
func probeWSLToWindowsLoopback(ctx context.Context, runner platform.WSLRunner) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("abrir listener temporário no Windows: %w", err)
	}
	defer listener.Close()

	serverResult := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverResult <- acceptErr
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
		_, writeErr := connection.Write([]byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n"))
		serverResult <- writeErr
	}()

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	const script = `if command -v curl >/dev/null 2>&1; then
    curl --silent --show-error --connect-timeout 1 --max-time 2 -o /dev/null "http://127.0.0.1:$1/"
elif command -v wget >/dev/null 2>&1; then
    wget -q -T 2 -O /dev/null "http://127.0.0.1:$1/"
else
    exit 127
fi`
	if _, err := runner.RunOperation(probeCtx, platform.WSLOperationDoctor, "/bin/sh", "-c", script, "devlan", port); err != nil {
		return fmt.Errorf("probe WSL→Windows em 127.0.0.1:%s: %w", port, err)
	}
	select {
	case serveErr := <-serverResult:
		if serveErr != nil {
			return fmt.Errorf("responder probe WSL→Windows: %w", serveErr)
		}
		return nil
	case <-probeCtx.Done():
		return probeCtx.Err()
	}
}

func probeWSLLANTCP(ctx context.Context, runner platform.WSLRunner, host string, port int) error {
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	const script = `timeout 2 /bin/bash -c '</dev/tcp/$1/$2' devlan "$1" "$2"`
	if _, err := runner.RunOperation(probeCtx, platform.WSLOperationDoctor, "/bin/bash", "-c", script, "devlan", host, strconv.Itoa(port)); err != nil {
		return err
	}
	return nil
}

// probeLANTCP gives mirrored WSL a short convergence window after the VM is
// restarted. systemd can report Caddy healthy before the host-side mirrored
// listener is reachable; treating that transient as a migration failure leaves
// the operator with an unnecessary rollback.
func probeLANTCP(ctx context.Context, host string, port int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(8 * time.Second)
	var lastErr error
	for {
		attemptContext, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
		lastErr = platform.ProbeTCP(attemptContext, host, port)
		cancel()
		if lastErr == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// MigrateToSingleCaddy applies the M8 topology in a deliberately explicit
// flow. The WSL shutdown is confirmation-gated because it stops every running
// distribution, not only the configured one.
