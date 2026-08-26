package platform

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("dependência não disponível")

type Runner interface {
	Run(ctx context.Context, args ...string) (string, error)
}

type ExecRunner struct {
	Program string
	Prefix  []string
}

func NewExecRunner(program string, prefix ...string) ExecRunner {
	return ExecRunner{Program: program, Prefix: append([]string(nil), prefix...)}
}

func (r ExecRunner) Run(ctx context.Context, args ...string) (string, error) {
	commandArgs := append(append([]string(nil), r.Prefix...), args...)
	command := exec.CommandContext(ctx, r.Program, commandArgs...)
	hideProcessWindow(command)
	output, err := command.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", ErrUnavailable, r.Program)
		}
		message := strings.TrimSpace(string(output))
		if message != "" {
			return "", fmt.Errorf("%s %v: %w: %s", r.Program, commandArgs, err, message)
		}
		return "", fmt.Errorf("%s %v: %w", r.Program, commandArgs, err)
	}
	return string(output), nil
}

type WSLRunner struct {
	Binary       string
	Distribution string
	// Invoker is injectable for deterministic tests and benchmarks. Production
	// leaves it nil, which invokes the configured wsl.exe binary.
	Invoker   Runner
	Stats     *WSLStats
	Execution *WSLExecutionCache
}

func NewWSLRunner(binary, distribution string) WSLRunner {
	if binary == "" {
		binary = "wsl.exe"
	}
	return WSLRunner{
		Binary:       binary,
		Distribution: distribution,
		Stats:        NewWSLStats(),
		Execution:    NewWSLExecutionCache(),
	}
}

func (r WSLRunner) Run(ctx context.Context, args ...string) (string, error) {
	operation := WSLOperation(ctx)
	if operation == "" {
		operation = WSLPlaneOperationUnclassified
	}
	return r.runWithOperation(ctx, operation, false, args...)
}

// RunOperation is the explicit form used by grouped operations. It keeps the
// inventory useful even when the caller's context came from another layer.
func (r WSLRunner) RunOperation(ctx context.Context, operation string, args ...string) (string, error) {
	return r.runWithOperation(ctx, operation, false, args...)
}

func (r WSLRunner) RunAsRoot(ctx context.Context, args ...string) (string, error) {
	operation := WSLOperation(ctx)
	if operation == "" {
		operation = WSLPlaneOperationUnclassified
	}
	return r.runWithOperation(ctx, operation, true, args...)
}

func (r WSLRunner) RunAsRootOperation(ctx context.Context, operation string, args ...string) (string, error) {
	return r.runWithOperation(ctx, operation, true, args...)
}

func (r WSLRunner) runWithOperation(ctx context.Context, operation string, asRoot bool, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	commandArgs := make([]string, 0, len(args)+4)
	if r.Distribution != "" {
		commandArgs = append(commandArgs, "--distribution", r.Distribution)
	}
	if asRoot {
		commandArgs = append(commandArgs, "--user", "root")
	}
	commandArgs = append(commandArgs, "--exec")
	commandArgs = append(commandArgs, args...)
	invoker := r.Invoker
	if invoker == nil {
		invoker = NewExecRunner(r.Binary)
	}
	started := time.Now()
	output, err := invoker.Run(ctx, commandArgs...)
	if err != nil {
		wrapped := wrapWSLError(operation, ctx, err)
		r.Stats.record(operation, started, ctx, wrapped)
		return "", wrapped
	}
	r.Stats.record(operation, started, ctx, nil)
	return output, nil
}

// GrantProjectAccess gives only the Caddy and PHP-FPM service accounts access
// to a registered WSL project. It avoids relaxing home-directory permissions.
func (r WSLRunner) GrantProjectAccess(ctx context.Context, projectPath string) error {
	return r.GrantProjectsAccess(ctx, projectPath)
}

// GrantProjectsAccess applies ACLs for all WSL projects in one root WSL
// invocation. Paths remain positional shell arguments; none is interpolated
// into the shell program.
func (r WSLRunner) GrantProjectsAccess(ctx context.Context, projectPaths ...string) error {
	paths := make([]string, 0, len(projectPaths))
	for _, projectPath := range projectPaths {
		if strings.HasPrefix(projectPath, "/") {
			paths = append(paths, projectPath)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	script := `set -e
for project in "$@"; do
    parent=$(/usr/bin/dirname -- "$project")
    while [ "$parent" != "/" ] && [ "$parent" != "." ]; do
        /usr/bin/setfacl -m 'u:caddy:--x,u:www-data:--x' -- "$parent"
        parent=$(/usr/bin/dirname -- "$parent")
    done
    /usr/bin/setfacl -R -m 'u:caddy:rX,u:www-data:rwX' -- "$project"
    /usr/bin/find "$project" -type d -exec /usr/bin/setfacl -m 'd:u:caddy:rX,d:u:www-data:rwX' -- {} +
done`
	args := []string{"/bin/sh", "-c", script, "devlan"}
	args = append(args, paths...)
	if _, err := r.RunAsRootOperation(ctx, WSLOperationAccess, args...); err != nil {
		return fmt.Errorf("conceder acesso aos serviços para projetos WSL: %w", err)
	}
	return nil
}

func (r WSLRunner) Exists(ctx context.Context, path string) (bool, error) {
	_, err := r.Run(ctx, "/usr/bin/test", "-e", path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrUnavailable) {
		return false, err
	}
	return false, nil
}

func (r WSLRunner) ReadFile(ctx context.Context, path string) ([]byte, error) {
	output, err := r.Run(ctx, "/bin/cat", path)
	if err != nil {
		return nil, err
	}
	return []byte(output), nil
}

// LaravelMarkers checks both Laravel marker files in one WSL process. Starting
// wsl.exe is comparatively expensive, so callers that discover many projects
// should prefer this over two individual Exists calls.
func (r WSLRunner) LaravelMarkers(ctx context.Context, projectPath string) (bool, bool, error) {
	output, err := r.RunOperation(ctx, WSLOperationDiscovery, "/bin/sh", "-c", `if [ -f "$1/artisan" ]; then printf 1; else printf 0; fi; if [ -f "$1/public/index.php" ]; then printf 1; else printf 0; fi`, "devlan", projectPath)
	if err != nil {
		return false, false, err
	}
	markers := strings.TrimSpace(output)
	return len(markers) == 2 && markers[0] == '1', len(markers) == 2 && markers[1] == '1', nil
}

func (r WSLRunner) IsSocket(ctx context.Context, path string) (bool, error) {
	_, err := r.Run(ctx, "/usr/bin/test", "-S", path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrUnavailable) {
		return false, err
	}
	return false, nil
}

func (r WSLRunner) HasCommand(ctx context.Context, command string) (bool, error) {
	_, err := r.Run(ctx, "/usr/bin/which", command)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrUnavailable) {
		return false, err
	}
	return false, nil
}

// HasCommands checks all binaries in one Linux shell. It is used by status,
// doctor and PHP discovery so the number of wsl.exe starts does not grow with
// the number of candidates.
func (r WSLRunner) HasCommands(ctx context.Context, commands ...string) (map[string]bool, error) {
	result := make(map[string]bool, len(commands))
	if len(commands) == 0 {
		return result, nil
	}
	for _, command := range commands {
		if strings.TrimSpace(command) == "" || strings.ContainsAny(command, "\r\n\t") {
			return nil, fmt.Errorf("comando WSL inválido: %q", command)
		}
	}
	script := `for command in "$@"; do
    if command -v "$command" >/dev/null 2>&1; then printf '1\n'; else printf '0\n'; fi
done`
	args := []string{"/bin/sh", "-c", script, "devlan"}
	args = append(args, commands...)
	output, err := r.RunOperation(ctx, wslOperationOr(ctx, WSLOperationStatus), args...)
	if err != nil {
		return nil, err
	}
	values := strings.Split(strings.TrimSpace(strings.ReplaceAll(output, "\r\n", "\n")), "\n")
	if len(values) != len(commands) {
		return nil, fmt.Errorf("resposta de disponibilidade WSL inválida: esperadas %d linhas, recebidas %d", len(commands), len(values))
	}
	for index, command := range commands {
		result[command] = values[index] == "1"
	}
	return result, nil
}

// IsSockets checks a set of PHP-FPM sockets in one invocation.
func (r WSLRunner) IsSockets(ctx context.Context, sockets ...string) (map[string]bool, error) {
	result := make(map[string]bool, len(sockets))
	if len(sockets) == 0 {
		return result, nil
	}
	for _, socket := range sockets {
		if strings.TrimSpace(socket) == "" || strings.ContainsAny(socket, "\r\n\t") {
			return nil, fmt.Errorf("socket WSL inválido: %q", socket)
		}
	}
	script := `for socket in "$@"; do
    if /usr/bin/test -S "$socket"; then printf '1\n'; else printf '0\n'; fi
done`
	args := []string{"/bin/sh", "-c", script, "devlan"}
	args = append(args, sockets...)
	output, err := r.RunOperation(ctx, wslOperationOr(ctx, WSLOperationStatus), args...)
	if err != nil {
		return nil, err
	}
	values := strings.Split(strings.TrimSpace(strings.ReplaceAll(output, "\r\n", "\n")), "\n")
	if len(values) != len(sockets) {
		return nil, fmt.Errorf("resposta de sockets WSL inválida: esperadas %d linhas, recebidas %d", len(sockets), len(values))
	}
	for index, socket := range sockets {
		result[socket] = values[index] == "1"
	}
	return result, nil
}

type WSLDevStatusRequest struct {
	Name    string
	PIDFile string
}

type WSLDevStatus struct {
	Name  string
	PID   int
	State DevProcessState
}

// DevStatuses reads all requested PID files in one WSL session. Whether the
// public port is accepting connections is checked by the Windows caller; WSL
// only supplies the process state that cannot be inferred from that socket.
func (r WSLRunner) DevStatuses(ctx context.Context, requests ...WSLDevStatusRequest) ([]WSLDevStatus, error) {
	result := make([]WSLDevStatus, 0, len(requests))
	if len(requests) == 0 {
		return result, nil
	}
	for _, request := range requests {
		if strings.TrimSpace(request.Name) == "" || strings.ContainsAny(request.Name, "\r\n\t") {
			return nil, fmt.Errorf("nome de projeto WSL inválido: %q", request.Name)
		}
		if !strings.HasPrefix(request.PIDFile, "/") || strings.ContainsAny(request.PIDFile, "\r\n\t") {
			return nil, fmt.Errorf("arquivo PID WSL inválido: %q", request.PIDFile)
		}
	}
	script := `while [ "$#" -ge 2 ]; do
    name="$1"
    pid_file="$2"
    shift 2
    pid=0
    state=stopped
    if [ -f "$pid_file" ]; then
        value=$(/bin/cat -- "$pid_file" 2>/dev/null || true)
        case "$value" in
            ''|*[!0-9]*) pid=0 ;;
            *) pid="$value" ;;
        esac
        if [ "$pid" -gt 0 ] && /bin/kill -0 "$pid" 2>/dev/null; then
            state=starting
        fi
    fi
    printf '%s\t%s\t%s\n' "$name" "$pid" "$state"
done`
	args := []string{"/bin/sh", "-c", script, "devlan"}
	for _, request := range requests {
		args = append(args, request.Name, request.PIDFile)
	}
	output, err := r.RunOperation(ctx, wslOperationOr(ctx, WSLOperationStatus), args...)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(output, "\r\n", "\n")), "\n")
	if len(lines) != len(requests) {
		return nil, fmt.Errorf("resposta de status dev WSL inválida: esperadas %d linhas, recebidas %d", len(requests), len(lines))
	}
	for index, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) != 3 || parts[0] != requests[index].Name {
			return nil, fmt.Errorf("linha de status dev WSL inválida: %q", line)
		}
		pid := 0
		if parsed, parseErr := strconv.Atoi(parts[1]); parseErr == nil && parsed > 0 {
			pid = parsed
		}
		state := DevProcessState(parts[2])
		if state != StateStarting && state != StateStopped {
			return nil, fmt.Errorf("estado de processo dev WSL inválido: %q", parts[2])
		}
		result = append(result, WSLDevStatus{Name: parts[0], PID: pid, State: state})
	}
	return result, nil
}

func (r WSLRunner) ListDirectories(ctx context.Context, parent string) ([]string, error) {
	output, err := r.RunOperation(ctx, WSLOperationDiscovery, "/usr/bin/find", parent, "-mindepth", "1", "-maxdepth", "1", "-type", "d", "-print")
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return nil, err
		}
		return []string{}, nil
	}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			paths = append(paths, filepath.ToSlash(trimmed))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

type DiscoveredRawProject struct {
	Path           string
	Artisan        bool
	PublicIndex    bool
	RootIndex      bool
	Console        bool
	HasPackageJSON bool
	PnpmLock       bool
	YarnLock       bool
	BunLock        bool
	NpmLock        bool
	Vite           bool
	Next           bool
	Nuxt           bool
	Astro          bool
	Svelte         bool
	DistHTML       bool
	DistDir        bool
	RootHTML       bool
}

func (r WSLRunner) DiscoverAllProjects(ctx context.Context, parent string) ([]DiscoveredRawProject, error) {
	script := `for d in "$1"/*; do
		if [ -d "$d" ]; then
			a=0; p=0; r=0; c=0
			pkg=0; pnpm=0; yarn=0; bun=0; npm=0
			vite=0; next=0; nuxt=0; astro=0; svelte=0
			dist_h=0; dist_d=0; root_h=0

			[ -f "$d/artisan" ] && a=1
			[ -f "$d/public/index.php" ] && p=1
			[ -f "$d/index.php" ] && r=1
			[ -f "$d/bin/console" ] && c=1

			[ -f "$d/package.json" ] && pkg=1
			[ -f "$d/pnpm-lock.yaml" ] && pnpm=1
			[ -f "$d/yarn.lock" ] && yarn=1
			([ -f "$d/bun.lockb" ] || [ -f "$d/bun.lock" ]) && bun=1
			[ -f "$d/package-lock.json" ] && npm=1

			([ -f "$d/vite.config.js" ] || [ -f "$d/vite.config.ts" ] || [ -f "$d/vite.config.mjs" ]) && vite=1
			([ -f "$d/next.config.js" ] || [ -f "$d/next.config.mjs" ] || [ -f "$d/next.config.ts" ]) && next=1
			([ -f "$d/nuxt.config.ts" ] || [ -f "$d/nuxt.config.js" ]) && nuxt=1
			([ -f "$d/astro.config.mjs" ] || [ -f "$d/astro.config.ts" ]) && astro=1
			[ -f "$d/svelte.config.js" ] && svelte=1

			[ -f "$d/dist/index.html" ] && dist_h=1
			[ -d "$d/dist" ] && dist_d=1
			[ -f "$d/index.html" ] && root_h=1

			printf '%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n' \
				"$d" "$a" "$p" "$r" "$c" "$pkg" "$pnpm" "$yarn" "$bun" "$npm" "$vite" "$next" "$nuxt" "$astro" "$svelte" "$dist_h" "$dist_d" "$root_h"
		fi
	done`
	output, err := r.RunOperation(ctx, WSLOperationDiscovery, "/bin/sh", "-c", script, "devlan", parent)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return nil, err
		}
		return []DiscoveredRawProject{}, nil
	}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	results := make([]DiscoveredRawProject, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(strings.TrimSpace(line), "\t")
		if len(parts) < 18 {
			continue
		}
		results = append(results, DiscoveredRawProject{
			Path:           filepath.ToSlash(parts[0]),
			Artisan:        parts[1] == "1",
			PublicIndex:    parts[2] == "1",
			RootIndex:      parts[3] == "1",
			Console:        parts[4] == "1",
			HasPackageJSON: parts[5] == "1",
			PnpmLock:       parts[6] == "1",
			YarnLock:       parts[7] == "1",
			BunLock:        parts[8] == "1",
			NpmLock:        parts[9] == "1",
			Vite:           parts[10] == "1",
			Next:           parts[11] == "1",
			Nuxt:           parts[12] == "1",
			Astro:          parts[13] == "1",
			Svelte:         parts[14] == "1",
			DistHTML:       parts[15] == "1",
			DistDir:        parts[16] == "1",
			RootHTML:       parts[17] == "1",
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Path < results[j].Path
	})
	return results, nil
}

func ToWSLPath(value string) (string, error) {
	clean := filepath.Clean(value)
	if runtime.GOOS != "windows" {
		return filepath.ToSlash(clean), nil
	}
	clean = filepath.ToSlash(clean)
	if len(clean) >= 3 && clean[1] == ':' {
		drive := strings.ToLower(string(clean[0]))
		return "/mnt/" + drive + "/" + strings.TrimPrefix(clean[2:], "/"), nil
	}
	if strings.HasPrefix(clean, "/") {
		return clean, nil
	}
	return "", fmt.Errorf("não foi possível converter caminho Windows para WSL: %q", value)
}
