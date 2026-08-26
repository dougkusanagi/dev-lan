//go:build !windows

package platform

import (
	"os"
	"os/exec"
)

func HideProcessWindow(command *exec.Cmd) {}

func hideProcessWindow(command *exec.Cmd) {}

func SpawnBackgroundDaemon(executable string, args []string) error {
	cmd := exec.Command(executable, args...)
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err == nil {
		cmd.Stdin = devNull
		cmd.Stdout = devNull
		cmd.Stderr = devNull
	}
	return cmd.Start()
}
