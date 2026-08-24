package platform

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
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
}

func NewWSLRunner(binary, distribution string) WSLRunner {
	if binary == "" {
		binary = "wsl.exe"
	}
	return WSLRunner{Binary: binary, Distribution: distribution}
}

func (r WSLRunner) Run(ctx context.Context, args ...string) (string, error) {
	commandArgs := make([]string, 0, len(args)+4)
	if r.Distribution != "" {
		commandArgs = append(commandArgs, "--distribution", r.Distribution)
	}
	commandArgs = append(commandArgs, "--exec")
	commandArgs = append(commandArgs, args...)
	return NewExecRunner(r.Binary).Run(ctx, commandArgs...)
}

// GrantProjectAccess gives only the Caddy and PHP-FPM service accounts access
// to a registered WSL project. It avoids relaxing home-directory permissions.
func (r WSLRunner) GrantProjectAccess(ctx context.Context, projectPath string) error {
	if !strings.HasPrefix(projectPath, "/") {
		return nil
	}
	ancestors := []string{}
	for parent := pathpkg.Dir(projectPath); parent != "/" && parent != "."; parent = pathpkg.Dir(parent) {
		ancestors = append(ancestors, parent)
	}
	args := []string{}
	if r.Distribution != "" {
		args = append(args, "--distribution", r.Distribution)
	}
	args = append(args, "--user", "root", "--exec", "/usr/bin/setfacl", "-m", "u:caddy:--x,u:www-data:--x")
	args = append(args, ancestors...)
	if _, err := NewExecRunner(r.Binary).Run(ctx, args...); err != nil {
		return fmt.Errorf("permitir travessia até %s: %w", projectPath, err)
	}
	args = args[:0]
	if r.Distribution != "" {
		args = append(args, "--distribution", r.Distribution)
	}
	args = append(args, "--user", "root", "--exec", "/usr/bin/setfacl", "-R", "-m", "u:caddy:rX,u:www-data:rwX", projectPath)
	if _, err := NewExecRunner(r.Binary).Run(ctx, args...); err != nil {
		return fmt.Errorf("conceder acesso aos serviços para %s: %w", projectPath, err)
	}
	args = args[:0]
	if r.Distribution != "" {
		args = append(args, "--distribution", r.Distribution)
	}
	args = append(args, "--user", "root", "--exec", "/usr/bin/find", projectPath, "-type", "d", "-exec", "/usr/bin/setfacl", "-m", "d:u:caddy:rX,d:u:www-data:rwX", "{}", "+")
	if _, err := NewExecRunner(r.Binary).Run(ctx, args...); err != nil {
		return fmt.Errorf("definir ACL padrão para %s: %w", projectPath, err)
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

// LaravelMarkers checks both Laravel marker files in one WSL process. Starting
// wsl.exe is comparatively expensive, so callers that discover many projects
// should prefer this over two individual Exists calls.
func (r WSLRunner) LaravelMarkers(ctx context.Context, projectPath string) (bool, bool, error) {
	output, err := r.Run(ctx, "/bin/sh", "-c", `if [ -f "$1/artisan" ]; then printf 1; else printf 0; fi; if [ -f "$1/public/index.php" ]; then printf 1; else printf 0; fi`, "devlan", projectPath)
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

func (r WSLRunner) ListDirectories(ctx context.Context, parent string) ([]string, error) {
	output, err := r.Run(ctx, "/usr/bin/find", parent, "-mindepth", "1", "-maxdepth", "1", "-type", "d", "-print")
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
