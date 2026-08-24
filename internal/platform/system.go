package platform

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

func LANAddress() (string, error) {
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
		if strings.Contains(strings.ToLower(output), "no rules") {
			return false, nil
		}
		return false, err
	}
	return !strings.Contains(strings.ToLower(output), "no rules"), nil
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
