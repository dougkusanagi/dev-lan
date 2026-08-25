//go:build !windows

package platform

import "os/exec"

func hideProcessWindow(command *exec.Cmd) {}
