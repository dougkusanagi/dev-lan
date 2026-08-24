package platform

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
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
