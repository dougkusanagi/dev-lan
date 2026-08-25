package platform

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

type DevProcessState string

const (
	StateStopped  DevProcessState = "stopped"
	StateStarting DevProcessState = "starting"
	StateRunning  DevProcessState = "running"
	StateError    DevProcessState = "error"
)

type DevProcessStatus struct {
	ProjectName string
	Port        int
	State       DevProcessState
	PID         int
	Output      string
}

type DevManager interface {
	StartDev(ctx context.Context, project domain.Project, port int, command string) error
	StopDev(ctx context.Context, project domain.Project, port int) error
	RestartDev(ctx context.Context, project domain.Project, port int, command string) error
	Status(ctx context.Context, project domain.Project, port int) (DevProcessStatus, error)
	InstallDeps(ctx context.Context, project domain.Project, pm string) (string, error)
	Build(ctx context.Context, project domain.Project, pm string) (string, error)
	Logs(ctx context.Context, project domain.Project, lines int) (string, error)
}

type WSLDevManager struct {
	WSL WSLRunner
}

func NewWSLDevManager(wsl WSLRunner) WSLDevManager {
	return WSLDevManager{WSL: wsl}
}

func (m WSLDevManager) usesWSL(projectPath string) bool {
	return runtime.GOOS == "windows" && strings.HasPrefix(projectPath, "/")
}

func (m WSLDevManager) devLogPath(project domain.Project) string {
	if m.usesWSL(project.Path) {
		return pathpkg.Join("/tmp", fmt.Sprintf("devlan-%s.log", project.Name))
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("devlan-%s.log", project.Name))
}

func (m WSLDevManager) devPIDPath(project domain.Project) string {
	if m.usesWSL(project.Path) {
		return pathpkg.Join("/tmp", fmt.Sprintf("devlan-%s.pid", project.Name))
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("devlan-%s.pid", project.Name))
}

func isPortListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}

func (m WSLDevManager) Status(ctx context.Context, project domain.Project, port int) (DevProcessStatus, error) {
	status := DevProcessStatus{
		ProjectName: project.Name,
		Port:        port,
		State:       StateStopped,
	}

	if isPortListening(port) {
		status.State = StateRunning
		return status, nil
	}

	// Check pid file
	if m.usesWSL(project.Path) {
		out, err := m.WSL.Run(ctx, "/bin/sh", "-c", fmt.Sprintf(`if [ -f "/tmp/devlan-%s.pid" ]; then cat "/tmp/devlan-%s.pid"; fi`, project.Name, project.Name))
		if err == nil && strings.TrimSpace(out) != "" {
			if pid, err := strconv.Atoi(strings.TrimSpace(out)); err == nil && pid > 0 {
				status.PID = pid
				// check if running
				_, err := m.WSL.Run(ctx, "/bin/kill", "-0", strconv.Itoa(pid))
				if err == nil {
					status.State = StateStarting
				}
			}
		}
	} else {
		pidFile := m.devPIDPath(project)
		data, err := os.ReadFile(pidFile)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				status.PID = pid
				status.State = StateStarting
			}
		}
	}

	return status, nil
}

func (m WSLDevManager) StartDev(ctx context.Context, project domain.Project, port int, command string) error {
	if isPortListening(port) {
		return nil
	}

	logFile := m.devLogPath(project)
	pidFile := m.devPIDPath(project)

	if command == "" {
		command = "npm run dev"
	}

	if m.usesWSL(project.Path) {
		// Launch background process inside WSL using nohup and recording PID
		script := fmt.Sprintf("cd %s && PORT=%d PORT_DEV=%d nohup /bin/sh -c %s > %s 2>&1 </dev/null & echo $! > %s",
			shellQuote(project.Path), port, port, shellQuote(command), shellQuote(logFile), shellQuote(pidFile))
		_, err := m.WSL.Run(ctx, "/bin/sh", "-c", script)
		if err != nil {
			return fmt.Errorf("iniciar servidor dev no WSL: %w", err)
		}
	} else {
		// Local process
		cmdParts := strings.Fields(command)
		if len(cmdParts) == 0 {
			cmdParts = []string{"npm", "run", "dev"}
		}
		cmd := exec.Command(cmdParts[0], cmdParts[1:]...)
		cmd.Dir = filepath.FromSlash(project.Path)
		cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", port))
		outFile, err := os.Create(logFile)
		if err == nil {
			cmd.Stdout = outFile
			cmd.Stderr = outFile
		}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("iniciar servidor dev local: %w", err)
		}
		if cmd.Process != nil {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
		}
	}

	// Do not report success until the server is actually accepting connections.
	// A background shell can exit successfully even when npm/bun fails.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if isPortListening(port) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}

	logOutput, _ := m.Logs(ctx, project, 80)
	message := strings.TrimSpace(logOutput)
	if message == "" {
		message = "nenhuma saída foi gravada"
	}
	if len(message) > 2000 {
		message = message[len(message)-2000:]
	}
	return fmt.Errorf("servidor dev não abriu a porta %d após 10s; logs: %s", port, message)
}

func (m WSLDevManager) StopDev(ctx context.Context, project domain.Project, port int) error {
	if m.usesWSL(project.Path) {
		// Kill by PID or port
		script := fmt.Sprintf(`
			if [ -f "/tmp/devlan-%s.pid" ]; then
				PID=$(cat "/tmp/devlan-%s.pid")
				if [ -n "$PID" ]; then
					kill -TERM "$PID" 2>/dev/null || kill -9 "$PID" 2>/dev/null
				fi
				rm -f "/tmp/devlan-%s.pid"
			fi
			fuser -k %d/tcp 2>/dev/null || true
		`, project.Name, project.Name, project.Name, port)
		_, _ = m.WSL.Run(ctx, "/bin/sh", "-c", script)
	} else {
		pidFile := m.devPIDPath(project)
		data, err := os.ReadFile(pidFile)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				if proc, err := os.FindProcess(pid); err == nil {
					_ = proc.Kill()
				}
			}
			_ = os.Remove(pidFile)
		}
	}
	return nil
}

func (m WSLDevManager) RestartDev(ctx context.Context, project domain.Project, port int, command string) error {
	_ = m.StopDev(ctx, project, port)
	time.Sleep(500 * time.Millisecond)
	return m.StartDev(ctx, project, port, command)
}

func (m WSLDevManager) InstallDeps(ctx context.Context, project domain.Project, pm string) (string, error) {
	if pm == "" {
		pm = "npm"
	}
	cmd := pm + " install"
	if m.usesWSL(project.Path) {
		// Keep the project path and package manager as positional arguments;
		// neither is interpolated into the shell program.
		return m.WSL.Run(ctx, "/bin/sh", "-c", `cd -- "$1" && exec "$2" install`, "devlan", project.Path, pm)
	}
	parts := strings.Fields(cmd)
	execCmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	execCmd.Dir = filepath.FromSlash(project.Path)
	out, err := execCmd.CombinedOutput()
	return string(out), err
}

func (m WSLDevManager) Build(ctx context.Context, project domain.Project, pm string) (string, error) {
	if pm == "" {
		pm = "npm"
	}
	cmd := pm + " run build"
	if m.usesWSL(project.Path) {
		return m.WSL.Run(ctx, "/bin/sh", "-c", `cd -- "$1" && exec "$2" run build`, "devlan", project.Path, pm)
	}
	parts := strings.Fields(cmd)
	execCmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	execCmd.Dir = filepath.FromSlash(project.Path)
	out, err := execCmd.CombinedOutput()
	return string(out), err
}

func (m WSLDevManager) Logs(ctx context.Context, project domain.Project, lines int) (string, error) {
	if lines <= 0 {
		lines = 100
	}
	if m.usesWSL(project.Path) {
		logFile := m.devLogPath(project)
		return m.WSL.Run(ctx, "/usr/bin/tail", "-n", strconv.Itoa(lines), logFile)
	}
	logFile := m.devLogPath(project)
	data, err := os.ReadFile(logFile)
	if err != nil {
		return "", err
	}
	allLines := strings.Split(string(data), "\n")
	if len(allLines) > lines {
		allLines = allLines[len(allLines)-lines:]
	}
	return strings.Join(allLines, "\n"), nil
}
