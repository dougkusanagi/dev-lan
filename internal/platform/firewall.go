package platform

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
)

const FirewallRuleName = "DevLAN"

// EnsureFirewall limits inbound HTTP to Windows' Private profile and the
// local subnet. netsh receives each argument separately; no user value is
// interpolated into a shell command.
func EnsureFirewall(ctx context.Context, port int) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("firewall DevLAN só é suportado no Windows")
	}
	_, err := NewExecRunner("netsh").Run(ctx,
		"advfirewall", "firewall", "add", "rule",
		"name="+FirewallRuleName,
		"dir=in",
		"action=allow",
		"protocol=TCP",
		"localport="+strconv.Itoa(port),
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
