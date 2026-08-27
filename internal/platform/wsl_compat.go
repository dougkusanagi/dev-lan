package platform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const MinimumMirroredWindowsBuild = 22621 // Windows 11 22H2

type CompatibilityStatus string

const (
	CompatibilityOK   CompatibilityStatus = "OK"
	CompatibilityWarn CompatibilityStatus = "WARN"
	CompatibilityFail CompatibilityStatus = "FAIL"
)

type CompatibilityCheck struct {
	Name   string              `json:"name"`
	Status CompatibilityStatus `json:"status"`
	Detail string              `json:"detail"`
}

type PortConflict struct {
	Port   int    `json:"port"`
	Detail string `json:"detail"`
}

// WSLCompatibilityReport is intentionally a diagnostic snapshot, not a
// persisted capability flag. Windows/WSL state can change after a reboot,
// VPN connection or network switch and must be probed again.
type WSLCompatibilityReport struct {
	Supported             bool                 `json:"supported"`
	WindowsVersion        string               `json:"windowsVersion,omitempty"`
	WindowsBuild          int                  `json:"windowsBuild,omitempty"`
	WSLVersion            string               `json:"wslVersion,omitempty"`
	WSL2                  bool                 `json:"wsl2"`
	MirroredConfigured    bool                 `json:"mirroredConfigured"`
	MirroredNetworking    bool                 `json:"mirroredNetworking"`
	Systemd               bool                 `json:"systemd"`
	LoopbackBidirectional bool                 `json:"loopbackBidirectional"`
	LANReachable          bool                 `json:"lanReachable"`
	PortConflicts         []PortConflict       `json:"portConflicts"`
	Checks                []CompatibilityCheck `json:"checks"`
}

// WSLCompatibilityProbe keeps the diagnostic testable and avoids shell
// interpolation. Outputs are obtained from fixed commands by the default
// implementation; tests can provide deterministic output and port probes.
type WSLCompatibilityProbe struct {
	Windows Runner
	WSL     Runner
	// WSLVersion is the host-level `wsl.exe` runner. It is separate from WSL,
	// which is normally scoped to `--distribution ... --exec` and therefore
	// cannot answer `wsl --version` or `wsl --list --verbose` itself.
	WSLVersion Runner
	// ConfigText is the host file read immediately before the probe. It is not
	// treated as proof of an effective mode unless the WSL-side probe also
	// reports the mode; callers can use it to explain a pending restart.
	ConfigText    string
	PortAvailable func(context.Context, int) bool
	LANProbe      func(context.Context) error
	// LoopbackProbe is the host-to-WSL half of the loopback check. The default
	// WSL-side probe tests WSL→Windows; callers with the Caddy adapter can add
	// this probe to ensure both directions are actually reachable.
	LoopbackProbe func(context.Context) error
	// WSLToWindowsProbe lets application callers verify WSL→Windows against a
	// temporary host listener. This avoids requiring the background DevLAN API
	// to already be running merely to validate or migrate the topology.
	WSLToWindowsProbe func(context.Context) error
}

func (p WSLCompatibilityProbe) windowsRunner() Runner {
	if p.Windows != nil {
		return p.Windows
	}
	return NewExecRunner("powershell.exe")
}

func (p WSLCompatibilityProbe) wslRunner() Runner {
	if p.WSL != nil {
		return p.WSL
	}
	return NewExecRunner("wsl.exe")
}

func (p WSLCompatibilityProbe) wslVersionRunner() Runner {
	if p.WSLVersion != nil {
		return p.WSLVersion
	}
	switch runner := p.WSL.(type) {
	case WSLRunner:
		if runner.Invoker != nil {
			return runner.Invoker
		}
		binary := runner.Binary
		if binary == "" {
			binary = "wsl.exe"
		}
		return NewExecRunner(binary)
	case *WSLRunner:
		if runner != nil {
			if runner.Invoker != nil {
				return runner.Invoker
			}
			binary := runner.Binary
			if binary == "" {
				binary = "wsl.exe"
			}
			return NewExecRunner(binary)
		}
	}
	return p.wslRunner()
}

func (p WSLCompatibilityProbe) runDistribution(ctx context.Context, distribution string, args ...string) (string, error) {
	runner := p.wslRunner()
	switch configured := runner.(type) {
	case WSLRunner:
		if distribution != "" {
			configured.Distribution = distribution
		}
		return configured.Run(ctx, args...)
	case *WSLRunner:
		if configured == nil {
			return "", fmt.Errorf("runner WSL não configurado")
		}
		copy := *configured
		if distribution != "" {
			copy.Distribution = distribution
		}
		return copy.Run(ctx, args...)
	default:
		commandArgs := make([]string, 0, len(args)+3)
		if distribution != "" {
			commandArgs = append(commandArgs, "--distribution", distribution)
		}
		commandArgs = append(commandArgs, "--exec")
		commandArgs = append(commandArgs, args...)
		return runner.Run(ctx, commandArgs...)
	}
}

func (p WSLCompatibilityProbe) Check(ctx context.Context, distribution string, ports ...int) WSLCompatibilityReport {
	report := WSLCompatibilityReport{Checks: []CompatibilityCheck{}, PortConflicts: []PortConflict{}}
	if ctx == nil {
		ctx = context.Background()
	}

	windowsOutput, windowsErr := p.windowsRunner().Run(ctx, "-NoProfile", "-NonInteractive", "-Command", "Get-ComputerInfo | Select-Object WindowsProductName,WindowsVersion,OsBuildNumber | Format-List | Out-String")
	if windowsErr != nil {
		report.Checks = append(report.Checks, CompatibilityCheck{"Windows 11 22H2+", CompatibilityFail, "não foi possível consultar a versão do Windows: " + windowsErr.Error()})
	} else {
		report.WindowsVersion = ParseWindowsVersion(windowsOutput)
		report.WindowsBuild = ParseWindowsBuild(windowsOutput)
		if report.WindowsBuild >= MinimumMirroredWindowsBuild {
			report.Checks = append(report.Checks, CompatibilityCheck{"Windows 11 22H2+", CompatibilityOK, fmt.Sprintf("build %d", report.WindowsBuild)})
		} else {
			report.Checks = append(report.Checks, CompatibilityCheck{"Windows 11 22H2+", CompatibilityFail, fmt.Sprintf("build %d; mínimo %d", report.WindowsBuild, MinimumMirroredWindowsBuild)})
		}
	}

	wslVersionRunner := p.wslVersionRunner()
	wslOutput, wslErr := wslVersionRunner.Run(ctx, "--version")
	if wslErr != nil {
		report.Checks = append(report.Checks, CompatibilityCheck{"WSL 2", CompatibilityFail, "WSL indisponível: " + wslErr.Error()})
	} else {
		report.WSLVersion = ParseWSLVersion(wslOutput)
		// `wsl --version` reports the WSL application version, not the
		// virtualization version of the selected distribution. Prefer the
		// authoritative `wsl --list --verbose` row and retain the old parser as
		// a compatibility fallback for older WSL builds/test doubles that do not
		// implement that command.
		distributionOutput, distributionErr := wslVersionRunner.Run(ctx, "--list", "--verbose")
		if distributionErr == nil {
			if version, found := ParseWSLDistributionVersion(distributionOutput, distribution); found {
				report.WSL2 = version == 2
			} else if strings.TrimSpace(distributionOutput) != "" {
				// A successful, non-empty list without the requested distribution
				// is authoritative: the WSL application exists, but this target
				// cannot be assumed to be WSL 2.
				report.WSL2 = false
			} else {
				report.WSL2 = IsWSL2Version(wslOutput)
			}
		} else {
			report.WSL2 = IsWSL2Version(wslOutput)
		}
		if report.WSL2 {
			report.Checks = append(report.Checks, CompatibilityCheck{"WSL 2", CompatibilityOK, report.WSLVersion})
		} else {
			detail := "a distribuição precisa executar no WSL 2"
			if distributionOutput != "" {
				detail = fmt.Sprintf("a distribuição %q não está no WSL 2", distribution)
			}
			report.Checks = append(report.Checks, CompatibilityCheck{"WSL 2", CompatibilityFail, detail})
		}
	}

	report.MirroredConfigured = WSLConfigHasMirroredNetworking(p.ConfigText)
	loopbackDetail := ""
	if effective, err := p.runDistribution(ctx, distribution, "/bin/sh", "-c", compatibilityProbeScript); err == nil {
		networkingMode, modeReported := ParseKeyValue(effective, "networkingMode")
		report.MirroredNetworking = modeReported && strings.EqualFold(networkingMode, "mirrored")
		report.Systemd = ParseKeyValueBool(effective, "systemd", "true")
		report.LoopbackBidirectional = ParseKeyValueBool(effective, "loopback", "true")
		if p.WSLToWindowsProbe != nil {
			if probeErr := p.WSLToWindowsProbe(ctx); probeErr != nil {
				report.LoopbackBidirectional = false
				loopbackDetail = probeErr.Error()
			} else {
				report.LoopbackBidirectional = true
			}
		}
		if !modeReported || strings.EqualFold(networkingMode, "unknown") {
			// WSL does not expose the selected mode in every release. A
			// successful WSL→Windows localhost probe is the observable
			// capability that mirrored mode adds; require the host setting too
			// so a configured-but-not-restarted VM is not reported healthy.
			report.MirroredNetworking = report.MirroredConfigured && report.LoopbackBidirectional
		}
		if report.LoopbackBidirectional && p.LoopbackProbe != nil {
			if probeErr := p.LoopbackProbe(ctx); probeErr != nil {
				report.LoopbackBidirectional = false
				loopbackDetail = probeErr.Error()
			}
		}
	} else {
		report.Checks = append(report.Checks, CompatibilityCheck{"Capacidades efetivas WSL", CompatibilityWarn, "não foi possível consultar a distribuição: " + err.Error()})
	}
	if report.MirroredNetworking {
		report.Checks = append(report.Checks, CompatibilityCheck{"networkingMode=mirrored", CompatibilityOK, "networking espelhada ativa"})
	} else if report.MirroredConfigured {
		report.Checks = append(report.Checks, CompatibilityCheck{"networkingMode=mirrored", CompatibilityWarn, "configurada, mas ainda não confirmada após reiniciar o WSL"})
	} else {
		report.Checks = append(report.Checks, CompatibilityCheck{"networkingMode=mirrored", CompatibilityFail, "ative networkingMode=mirrored em .wslconfig e reinicie o WSL"})
	}
	appendCapabilityCheck(&report, "systemd", report.Systemd, "systemd ativo", "ative systemd na distribuição e reinicie o WSL")
	if report.LoopbackBidirectional {
		report.Checks = append(report.Checks, CompatibilityCheck{"loopback bidirecional", CompatibilityOK, "loopback Windows↔WSL disponível"})
	} else if loopbackDetail != "" {
		report.Checks = append(report.Checks, CompatibilityCheck{"loopback bidirecional", CompatibilityFail, loopbackDetail})
	} else {
		report.Checks = append(report.Checks, CompatibilityCheck{"loopback bidirecional", CompatibilityFail, "o Caddy WSL precisa alcançar 127.0.0.1 da API Windows"})
	}

	if p.LANProbe == nil {
		// A missing probe is not evidence that the mirrored interface is
		// reachable from the LAN. Reporting success here made a fresh or
		// partially configured installation look healthy before any listener was
		// tested.
		report.Checks = append(report.Checks, CompatibilityCheck{"Acesso LAN", CompatibilityWarn, "probe LAN não configurado"})
	} else if err := p.LANProbe(ctx); err != nil {
		report.Checks = append(report.Checks, CompatibilityCheck{"Acesso LAN", CompatibilityWarn, err.Error()})
	} else {
		report.LANReachable = true
	}
	if report.LANReachable {
		report.Checks = append(report.Checks, CompatibilityCheck{"Acesso LAN", CompatibilityOK, "probe de rede disponível"})
	} else {
		report.Checks = append(report.Checks, CompatibilityCheck{"Acesso LAN", CompatibilityWarn, "não foi possível confirmar o caminho LAN"})
	}

	seen := map[int]bool{}
	for _, port := range ports {
		if port < 1 || port > 65535 || seen[port] {
			continue
		}
		seen[port] = true
		available := true
		if p.PortAvailable != nil {
			available = p.PortAvailable(ctx, port)
		} else {
			available = IsPortAvailable(port)
		}
		if !available {
			report.PortConflicts = append(report.PortConflicts, PortConflict{Port: port, Detail: "porta ocupada por outro listener"})
		}
	}
	sort.Slice(report.PortConflicts, func(i, j int) bool { return report.PortConflicts[i].Port < report.PortConflicts[j].Port })
	if len(report.PortConflicts) == 0 {
		report.Checks = append(report.Checks, CompatibilityCheck{"Portas 80/443/pool", CompatibilityOK, "nenhum conflito detectado"})
	} else {
		report.Checks = append(report.Checks, CompatibilityCheck{"Portas 80/443/pool", CompatibilityFail, formatPortConflicts(report.PortConflicts)})
	}

	report.Supported = report.WindowsBuild >= MinimumMirroredWindowsBuild && report.WSL2 &&
		report.MirroredNetworking && report.Systemd && report.LoopbackBidirectional && report.LANReachable && len(report.PortConflicts) == 0
	return report
}

const compatibilityProbeScript = `printf 'networkingMode=unknown\n'
if [ "$(ps -p 1 -o comm= 2>/dev/null | tr -d ' ')" = "systemd" ]; then printf 'systemd=true\n'; else printf 'systemd=false\n'; fi
loopback=false
if command -v curl >/dev/null 2>&1; then
    if curl --silent --max-time 1 http://127.0.0.1:3210/v1/health >/dev/null 2>&1; then loopback=true; fi
elif command -v wget >/dev/null 2>&1; then
    if wget -q -T 1 -t 1 -O /dev/null http://127.0.0.1:3210/v1/health; then loopback=true; fi
fi
printf 'loopback=%s\n' "$loopback"`

func appendCapabilityCheck(report *WSLCompatibilityReport, name string, ok bool, success, failure string) {
	if ok {
		report.Checks = append(report.Checks, CompatibilityCheck{name, CompatibilityOK, success})
	} else {
		report.Checks = append(report.Checks, CompatibilityCheck{name, CompatibilityFail, failure})
	}
}

func formatPortConflicts(conflicts []PortConflict) string {
	parts := make([]string, 0, len(conflicts))
	for _, conflict := range conflicts {
		parts = append(parts, strconv.Itoa(conflict.Port))
	}
	return "ocupadas: " + strings.Join(parts, ", ")
}

var windowsBuildPattern = regexp.MustCompile(`(?i)(?:osbuildnumber|build(?:\s+number)?|10\.0\.)\s*[:=]?\s*(?:10\.0\.)?(\d{4,6})`)
var windowsVersionPattern = regexp.MustCompile(`(?i)windows(?:productname|version)?\s*[:=]\s*([^\r\n]+)`)
var wslVersionPattern = regexp.MustCompile(`(?im)(?:wsl\s+version|versão\s+do\s+wsl)\s*:\s*([^\r\n]+)`)

func ParseWindowsBuild(output string) int {
	if match := windowsBuildPattern.FindStringSubmatch(output); len(match) > 1 {
		build, _ := strconv.Atoi(match[1])
		return build
	}
	// `ver` and a few older PowerShell outputs contain only 10.0.<build>.
	parts := regexp.MustCompile(`10\.0\.(\d{4,6})`).FindStringSubmatch(output)
	if len(parts) > 1 {
		build, _ := strconv.Atoi(parts[1])
		return build
	}
	return 0
}

func ParseWindowsVersion(output string) string {
	if match := windowsVersionPattern.FindStringSubmatch(output); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func ParseWSLVersion(output string) string {
	if match := wslVersionPattern.FindStringSubmatch(output); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if strings.Contains(strings.ToLower(line), "version") && strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return strings.TrimSpace(output)
}

func IsWSL2Version(output string) bool {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "version: 1") || strings.Contains(lower, "version 1") || strings.Contains(lower, "versão do wsl: 1") || strings.Contains(lower, "versão do wsl 1") {
		return false
	}
	return strings.Contains(lower, "wsl version") || strings.Contains(lower, "versão do wsl") || strings.Contains(lower, "version: 2") || strings.Contains(lower, "version 2") || strings.Contains(lower, "wsl 2")
}

// ParseWSLDistributionVersion extracts the VERSION column from
// `wsl --list --verbose`. The command can emit a leading `*` for the default
// distribution and may contain NUL bytes when invoked from a Windows process.
// Distribution names are compared case-insensitively; when no distribution is
// requested, the first version-bearing row is used.
func ParseWSLDistributionVersion(output, distribution string) (version int, found bool) {
	target := strings.TrimSpace(strings.ReplaceAll(distribution, "\x00", ""))
	output = strings.ReplaceAll(strings.ReplaceAll(output, "\r\n", "\n"), "\x00", "")
	for _, raw := range strings.Split(output, "\n") {
		fields := strings.Fields(raw)
		if len(fields) < 2 {
			continue
		}
		last := fields[len(fields)-1]
		parsed, err := strconv.Atoi(last)
		if err != nil || (parsed != 1 && parsed != 2) {
			continue
		}
		name := fields[0]
		if name == "*" && len(fields) > 1 {
			name = fields[1]
		}
		if target != "" && !strings.EqualFold(strings.TrimSpace(name), target) {
			continue
		}
		return parsed, true
	}
	return 0, false
}

func ParseKeyValueBool(output, key, expected string) bool {
	value, ok := ParseKeyValue(output, key)
	return ok && strings.EqualFold(strings.TrimSpace(value), expected)
}

func ParseKeyValue(output, key string) (string, bool) {
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		parts := strings.SplitN(strings.TrimSpace(raw), "=", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), key) {
			return strings.TrimSpace(parts[1]), true
		}
	}
	return "", false
}

// CheckWSLCompatibility is the convenient production entry point. It uses
// fixed host/WSL commands and includes every port that must be bound by the
// unified Caddy instance.
func CheckWSLCompatibility(ctx context.Context, distribution string, routeBase, routeCount int) WSLCompatibilityReport {
	ports := []int{80, 443}
	if routeCount > 0 && routeBase > 0 {
		for port := routeBase; port < routeBase+routeCount && port <= 65535; port++ {
			ports = append(ports, port)
		}
	}
	return (WSLCompatibilityProbe{}).Check(ctx, distribution, ports...)
}

var ErrWSLShutdownConfirmation = errors.New("a interrupção do WSL exige confirmação explícita")
