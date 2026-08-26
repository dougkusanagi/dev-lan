//go:build windows

package platform

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func HideProcessWindow(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000 | 0x00000200, // CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP
	}
}

func hideProcessWindow(command *exec.Cmd) {
	HideProcessWindow(command)
}

func SpawnBackgroundDaemon(executable string, args []string) error {
	formattedArgs := make([]string, len(args))
	for i, arg := range args {
		formattedArgs[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(arg, "'", "''"))
	}
	psCommand := fmt.Sprintf("Start-Process -FilePath '%s' -ArgumentList @(%s) -WindowStyle Hidden", strings.ReplaceAll(executable, "'", "''"), strings.Join(formattedArgs, ","))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}
