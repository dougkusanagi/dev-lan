package platform

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

func LANAddress() (string, error) {
	// The outbound route normally selects the physical LAN interface and avoids
	// virtual adapters such as WSL, Hyper-V and Docker being chosen first.
	if connection, err := net.Dial("udp4", "8.8.8.8:80"); err == nil {
		if address, ok := connection.LocalAddr().(*net.UDPAddr); ok && address.IP.To4() != nil && !address.IP.IsLoopback() {
			_ = connection.Close()
			return address.IP.String(), nil
		}
		_ = connection.Close()
	}

	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	var private []string
	var other []string
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 || networkInterface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || ip.To4() == nil || ip.IsLoopback() {
				continue
			}
			if isPrivateIPv4(ip) {
				private = append(private, ip.String())
			} else {
				other = append(other, ip.String())
			}
		}
	}
	sort.Strings(private)
	sort.Strings(other)
	if len(private) > 0 {
		return private[0], nil
	}
	if len(other) > 0 {
		return other[0], nil
	}
	return "", errors.New("nenhum endereço IPv4 de rede local encontrado")
}

func isPrivateIPv4(ip net.IP) bool {
	return ip.IsPrivate() || ip.Equal(net.ParseIP("127.0.0.1"))
}

func IsPortAvailable(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

// ListeningTCPPorts returns ports already occupied by host listeners. It is
// intentionally a Windows adapter: WSL project listeners are not host route
// listeners and are supplied separately by the application reservations.
// Tests inject a snapshot directly into App.ExternalListeners.
func ListeningTCPPorts(ctx context.Context) ([]int, error) {
	if runtime.GOOS != "windows" {
		return nil, nil
	}
	out, err := NewExecRunner("netstat").Run(ctx, "-ano", "-p", "tcp")
	if err != nil {
		return nil, err
	}
	seen := map[int]struct{}{}
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.EqualFold(fields[0], "TCP") || !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		local := fields[1]
		separator := strings.LastIndexByte(local, ':')
		if separator < 0 {
			continue
		}
		port, parseErr := strconv.Atoi(local[separator+1:])
		if parseErr == nil && port >= 1 && port <= 65535 {
			seen[port] = struct{}{}
		}
	}
	ports := make([]int, 0, len(seen))
	for port := range seen {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports, nil
}

func IsAdminResponsive(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func FirewallRule(ctx context.Context, name string) (bool, error) {
	if runtime.GOOS != "windows" {
		return false, fmt.Errorf("firewall Windows não se aplica a %s", runtime.GOOS)
	}
	output, err := NewExecRunner("netsh").Run(ctx, "advfirewall", "firewall", "show", "rule", "name="+name)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return false, err
		}
		// netsh uses a non-zero exit code for a missing rule on some versions.
		if firewallOutputHasNoRules(output) {
			return false, nil
		}
		return false, err
	}
	return !firewallOutputHasNoRules(output), nil
}

// FirewallRuleExists is the descriptive alias retained for newer callers.
func FirewallRuleExists(ctx context.Context, name string) (bool, error) {
	return FirewallRule(ctx, name)
}

func OpenURL(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	case "darwin":
		command = exec.Command("open", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("abrir %s: %w", url, err)
	}
	return nil
}

func NetworkProfile(ctx context.Context) (isPublic bool, detail string, err error) {
	if runtime.GOOS == "windows" {
		out, runErr := NewExecRunner("powershell.exe").Run(ctx, "-NoProfile", "-NonInteractive", "-Command", "Get-NetConnectionProfile | Select-Object -ExpandProperty NetworkCategory")
		if runErr == nil {
			lines := strings.Split(strings.TrimSpace(out), "\n")
			for _, line := range lines {
				l := strings.ToLower(strings.TrimSpace(line))
				if strings.Contains(l, "public") {
					return true, "Public (rede pública detectada)", nil
				}
			}
			return false, "Private", nil
		}
	}
	ip, lanErr := LANAddress()
	if lanErr == nil {
		parsed := net.ParseIP(ip)
		if parsed != nil && !isPrivateIPv4(parsed) {
			return true, fmt.Sprintf("IP público na interface LAN (%s)", ip), nil
		}
	}
	return false, "Private", nil
}

func FindCARootCertPath() string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		p := filepath.Join(appData, "Caddy", "pki", "authorities", "local", "root.crt")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates := []string{
			filepath.Join(home, "AppData", "Roaming", "Caddy", "pki", "authorities", "local", "root.crt"),
			filepath.Join(home, ".local", "share", "caddy", "pki", "authorities", "local", "root.crt"),
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}
