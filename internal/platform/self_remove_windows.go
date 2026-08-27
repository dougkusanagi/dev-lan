//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ScheduleDeferredRemoval starts a detached PowerShell helper that waits for
// this process to exit and then removes the exact files/directories supplied.
// Windows keeps an executable open while it runs, so attempting to delete the
// CLI synchronously would leave a half-uninstalled tree.
func ScheduleDeferredRemoval(targets ...string) error {
	cleanTargets := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" || strings.ContainsAny(target, "\r\n\x00") {
			continue
		}
		clean, err := filepath.Abs(target)
		if err != nil {
			return fmt.Errorf("caminho de autolimpeza inválido: %w", err)
		}
		clean = filepath.Clean(clean)
		key := strings.ToUpper(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleanTargets = append(cleanTargets, clean)
	}
	if len(cleanTargets) == 0 {
		return nil
	}
	workDir := os.TempDir()
	if strings.TrimSpace(workDir) == "" {
		return errors.New("diretório temporário indisponível para autolimpeza")
	}
	helper, err := os.CreateTemp(workDir, "devlan-uninstall-*.ps1")
	if err != nil {
		return fmt.Errorf("criar helper de autolimpeza: %w", err)
	}
	helperPath := helper.Name()
	removeHelper := true
	defer func() {
		_ = helper.Close()
		if removeHelper {
			_ = os.Remove(helperPath)
		}
	}()
	const script = `param(
    [Parameter(Mandatory=$true)][int]$ParentPid,
    [Parameter(Mandatory=$true)][string[]]$Targets,
    [Parameter(Mandatory=$true)][string]$ScriptPath
)
$deadline = (Get-Date).AddMinutes(5)
while ((Get-Date) -lt $deadline) {
    if (-not (Get-Process -Id $ParentPid -ErrorAction SilentlyContinue)) { break }
    Start-Sleep -Milliseconds 250
}
foreach ($target in $Targets) {
    try {
        if (Test-Path -LiteralPath $target) {
            Remove-Item -LiteralPath $target -Recurse -Force -ErrorAction SilentlyContinue
        }
    } catch {}
}
try { Remove-Item -LiteralPath $ScriptPath -Force -ErrorAction SilentlyContinue } catch {}
`
	if _, err := helper.WriteString(script); err != nil {
		return fmt.Errorf("gravar helper de autolimpeza: %w", err)
	}
	if err := helper.Close(); err != nil {
		return fmt.Errorf("fechar helper de autolimpeza: %w", err)
	}
	args := []string{"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", helperPath, "-ParentPid", strconv.Itoa(os.Getpid()), "-Targets"}
	args = append(args, cleanTargets...)
	args = append(args, "-ScriptPath", helperPath)
	command := exec.Command("powershell.exe", args...)
	command.Stdout = nil
	command.Stderr = nil
	hideProcessWindow(command)
	if err := command.Start(); err != nil {
		return fmt.Errorf("iniciar helper de autolimpeza: %w", err)
	}
	removeHelper = false
	_ = command.Process.Release()
	return nil
}
