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

type DevStatusRequest struct {
	Project domain.Project
	Port    int
}

// DevStatusBatcher is optional so existing managers and test doubles remain
// valid. The WSL implementation uses it to inspect all Linux PID files in a
// single wsl.exe session.
type DevStatusBatcher interface {
	StatusBatch(ctx context.Context, requests []DevStatusRequest) ([]DevProcessStatus, error)
}

type WSLDevManager struct {
	WSL WSLRunner
}

func NewWSLDevManager(wsl WSLRunner) WSLDevManager {
	return WSLDevManager{WSL: wsl}
}

var viteConfigNames = []string{
	"vite.config.js",
	"vite.config.mjs",
	"vite.config.cjs",
	"vite.config.ts",
	"vite.config.mts",
	"vite.config.cts",
}

const devStartupTimeout = 30 * time.Second

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

func (m WSLDevManager) usesVite(ctx context.Context, project domain.Project) bool {
	if project.DevFramework != nil && strings.EqualFold(strings.TrimSpace(*project.DevFramework), "vite") {
		return true
	}
	if m.usesWSL(project.Path) {
		args := append([]string{"/bin/sh", "-c", `for file in "$@"; do [ -f "$file" ] && { printf vite; exit 0; }; done`, "devlan"}, viteConfigPaths(project.Path)...)
		output, err := m.WSL.Run(ctx, args...)
		return err == nil && strings.TrimSpace(output) == "vite"
	}
	for _, name := range viteConfigNames {
		if info, err := os.Stat(filepath.Join(filepath.FromSlash(project.Path), name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func viteConfigPaths(projectPath string) []string {
	paths := make([]string, 0, len(viteConfigNames))
	for _, name := range viteConfigNames {
		paths = append(paths, pathpkg.Join(projectPath, name))
	}
	return paths
}

func viteCommand(command string, port int) string {
	command = strings.TrimSpace(command)
	if vitePortSpecified(command) {
		return command
	}

	args := []string{}
	if !viteHostSpecified(command) {
		args = append(args, "--host", "0.0.0.0")
	}
	args = append(args, "--port", strconv.Itoa(port))

	fields := strings.Fields(command)
	if len(fields) >= 3 && (fields[0] == "npm" || fields[0] == "pnpm" || fields[0] == "bun") && fields[1] == "run" {
		if strings.Contains(command, " -- ") {
			return command + " " + strings.Join(args, " ")
		}
		return command + " -- " + strings.Join(args, " ")
	}
	return command + " " + strings.Join(args, " ")
}

func vitePortSpecified(command string) bool {
	for _, field := range strings.Fields(command) {
		if field == "--port" || field == "-p" || strings.HasPrefix(field, "--port=") || (strings.HasPrefix(field, "-p") && len(field) > 2) {
			return true
		}
	}
	return false
}

func viteHostSpecified(command string) bool {
	for _, field := range strings.Fields(command) {
		if field == "--host" || field == "-h" || strings.HasPrefix(field, "--host=") {
			return true
		}
	}
	return false
}

func (m WSLDevManager) configureViteHotFile(ctx context.Context, project domain.Project) error {
	hotFile := pathpkg.Join(project.Path, "public", "hot")
	if m.usesWSL(project.Path) {
		_, err := m.WSL.Run(ctx, "/bin/sh", "-c", `printf 'https://%s.localhost\n' "$1" > "$2"`, "devlan", project.Name, hotFile)
		return err
	}
	return os.WriteFile(filepath.Join(filepath.FromSlash(project.Path), "public", "hot"), []byte("https://"+project.Name+".localhost\n"), 0o644)
}

func devUnitName(projectName string) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(projectName) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9', char == '-', char == '_':
			builder.WriteRune(char)
		default:
			builder.WriteRune('-')
		}
	}
	name := strings.Trim(builder.String(), "-_")
	if name == "" {
		name = "project"
	}
	return "devlan-dev-" + name
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
	items, err := m.StatusBatch(ctx, []DevStatusRequest{{Project: project, Port: port}})
	if err != nil {
		return DevProcessStatus{}, err
	}
	if len(items) == 0 {
		return DevProcessStatus{ProjectName: project.Name, Port: port, State: StateStopped}, nil
	}
	return items[0], nil
}

func (m WSLDevManager) StatusBatch(ctx context.Context, requests []DevStatusRequest) ([]DevProcessStatus, error) {
	result := make([]DevProcessStatus, len(requests))
	wslRequests := make([]WSLDevStatusRequest, 0, len(requests))
	wslIndexes := make(map[string]int, len(requests))
	for index, request := range requests {
		result[index] = DevProcessStatus{
			ProjectName: request.Project.Name,
			Port:        request.Port,
			State:       StateStopped,
		}
		if isPortListening(request.Port) {
			result[index].State = StateRunning
			continue
		}
		if m.usesWSL(request.Project.Path) {
			wslIndexes[request.Project.Name] = index
			wslRequests = append(wslRequests, WSLDevStatusRequest{
				Name:    request.Project.Name,
				PIDFile: m.devPIDPath(request.Project),
			})
			continue
		}
		data, err := os.ReadFile(m.devPIDPath(request.Project))
		if err == nil {
			if pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data))); parseErr == nil && pid > 0 {
				result[index].PID = pid
				result[index].State = StateStarting
			}
		}
	}
	if len(wslRequests) == 0 {
		return result, nil
	}
	items, err := m.WSL.DevStatuses(ctx, wslRequests...)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if index, ok := wslIndexes[item.Name]; ok {
			result[index].PID = item.PID
			result[index].State = item.State
		}
	}
	return result, nil
}

func (m WSLDevManager) StartDev(ctx context.Context, project domain.Project, port int, command string) error {
	isVite := m.usesVite(ctx, project)
	if isPortListening(port) {
		if isVite {
			if err := m.configureViteHotFile(ctx, project); err != nil {
				return fmt.Errorf("configurar URL pública do Vite: %w", err)
			}
		}
		return nil
	}

	logFile := m.devLogPath(project)
	pidFile := m.devPIDPath(project)

	if command == "" {
		command = "npm run dev"
	}
	if isVite {
		command = viteCommand(command, port)
	}

	if m.usesWSL(project.Path) {
		// WSL can terminate ordinary background jobs when the wsl.exe session
		// exits. Prefer a user systemd unit when available, with nohup as the
		// fallback for distributions without systemd.
		unit := devUnitName(project.Name)
		script := `
PROJECT="$1"
COMMAND="$2"
LOG="$3"
PIDFILE="$4"
UNIT="$5"
PORT="$6"

if command -v systemd-run >/dev/null 2>&1 && systemctl --user is-system-running >/dev/null 2>&1; then
    systemctl --user stop "$UNIT" >/dev/null 2>&1 || true
    rm -f "$PIDFILE"
    if systemd-run --user --unit="$UNIT" --collect --working-directory="$PROJECT" \
        --setenv="PORT=$PORT" --setenv="PORT_DEV=$PORT" \
        /bin/sh -c 'exec /bin/sh -c "$1" >"$2" 2>&1' devlan "$COMMAND" "$LOG" >/dev/null 2>&1; then
        for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
            PID=$(systemctl --user show --property=MainPID --value "$UNIT" 2>/dev/null || true)
            case "$PID" in
                ''|0) sleep 0.1 ;;
                *) printf '%s\n' "$PID" > "$PIDFILE"; exit 0 ;;
            esac
        done
        systemctl --user stop "$UNIT" >/dev/null 2>&1 || true
    fi
fi

cd -- "$PROJECT" && PORT="$PORT" PORT_DEV="$PORT" nohup /bin/sh -c "$COMMAND" >"$LOG" 2>&1 </dev/null &
printf '%s\n' "$!" > "$PIDFILE"
`
		_, err := m.WSL.Run(ctx, "/bin/sh", "-c", script, "devlan", project.Path, command, logFile, pidFile, unit, strconv.Itoa(port))
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
	deadline := time.Now().Add(devStartupTimeout)
	for time.Now().Before(deadline) {
		if isPortListening(port) {
			if isVite {
				if err := m.configureViteHotFile(ctx, project); err != nil {
					return fmt.Errorf("configurar URL pública do Vite: %w", err)
				}
			}
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
	return fmt.Errorf("servidor dev não abriu a porta %d após %s; logs: %s", port, devStartupTimeout, message)
}

func (m WSLDevManager) StopDev(ctx context.Context, project domain.Project, port int) error {
	if m.usesWSL(project.Path) {
		// Kill by PID or port
		script := fmt.Sprintf(`
			systemctl --user stop %s >/dev/null 2>&1 || true
			if [ -f "/tmp/devlan-%s.pid" ]; then
				PID=$(cat "/tmp/devlan-%s.pid")
				if [ -n "$PID" ]; then
					kill -TERM "$PID" 2>/dev/null || kill -9 "$PID" 2>/dev/null
				fi
				rm -f "/tmp/devlan-%s.pid"
			fi
			fuser -k %d/tcp 2>/dev/null || true
			rm -f %s
		`, shellQuote(devUnitName(project.Name)), project.Name, project.Name, project.Name, port, shellQuote(pathpkg.Join(project.Path, "public", "hot")))
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
		_ = os.Remove(filepath.Join(filepath.FromSlash(project.Path), "public", "hot"))
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
