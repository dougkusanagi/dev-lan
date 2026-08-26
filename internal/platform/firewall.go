package platform

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// FirewallManager is the application boundary for the host firewall. Keeping
// it injectable lets the transaction coordinator and doctor be tested without
// invoking netsh or requiring elevation.
type FirewallManager interface {
	Ensure(context.Context, ...int) error
	Remove(context.Context) error
}

type SystemFirewall struct{}

func (SystemFirewall) Ensure(ctx context.Context, ports ...int) error {
	return EnsureFirewall(ctx, ports...)
}
func (SystemFirewall) Remove(ctx context.Context) error { return RemoveFirewall(ctx) }

const FirewallRuleName = "DevLAN"

// EnsureFirewall limits inbound DevLAN HTTP/HTTPS to Windows' Private profile
// and the local subnet. netsh receives each argument separately; no user value
// is interpolated into a shell command.
func EnsureFirewall(ctx context.Context, ports ...int) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("firewall DevLAN só é suportado no Windows")
	}
	if len(ports) == 0 {
		return fmt.Errorf("ao menos uma porta do firewall é obrigatória")
	}
	unique := map[int]struct{}{}
	for _, port := range ports {
		if port < 1 || port > 65535 {
			return fmt.Errorf("porta do firewall inválida: %d", port)
		}
		unique[port] = struct{}{}
	}
	ports = ports[:0]
	for port := range unique {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	values := make([]string, 0, len(ports))
	for _, port := range ports {
		values = append(values, strconv.Itoa(port))
	}
	localPorts := strings.Join(values, ",")
	runner := NewExecRunner("netsh")
	if _, err := runner.Run(ctx,
		"advfirewall", "firewall", "set", "rule",
		"name="+FirewallRuleName,
		"new",
		"localport="+localPorts,
	); err == nil {
		return nil
	}
	_, err := runner.Run(ctx,
		"advfirewall", "firewall", "add", "rule",
		"name="+FirewallRuleName,
		"dir=in",
		"action=allow",
		"protocol=TCP",
		"localport="+localPorts,
		"profile=private",
		"remoteip=localsubnet",
	)
	return err
}

func RemoveFirewall(ctx context.Context) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	_, err := NewExecRunner("netsh").Run(ctx,
		"advfirewall", "firewall", "delete", "rule", "name="+FirewallRuleName,
	)
	return err
}
