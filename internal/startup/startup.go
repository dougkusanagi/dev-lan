package startup

import (
	"errors"
	"fmt"
	"strings"
)

const RunKey = `Software\Microsoft\Windows\CurrentVersion\Run`

type Mode string

const (
	ModeGUI     Mode = "gui"
	ModeService Mode = "service"
)

var ErrUnsupported = errors.New("inicialização automática só é suportada no Windows")

type State struct {
	Enabled bool   `json:"enabled"`
	Mode    Mode   `json:"mode,omitempty"`
	Command string `json:"command,omitempty"`
}

func ParseMode(value string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(value)))
	if mode != ModeGUI && mode != ModeService {
		return "", fmt.Errorf("modo de inicialização inválido %q (use gui ou service)", value)
	}
	return mode, nil
}

func BuildCommand(executable, dataDir string, mode Mode) (string, error) {
	if strings.TrimSpace(executable) == "" || strings.TrimSpace(dataDir) == "" {
		return "", errors.New("executável e diretório de dados são obrigatórios")
	}
	if strings.ContainsAny(executable, "\r\n\"") || strings.ContainsAny(dataDir, "\r\n\"") {
		return "", errors.New("executável ou diretório de dados contém aspas ou quebras de linha")
	}
	if mode != ModeGUI && mode != ModeService {
		return "", fmt.Errorf("modo de inicialização inválido: %q", mode)
	}
	command := string(ModeGUI)
	if mode == ModeService {
		// HKCU\Run launches an interactive process, not an SCM service. The
		// API server is the correct fallback when login startup is requested
		// without registering the executable with the Windows service manager.
		command = "api serve"
	}
	return fmt.Sprintf(`"%s" --data-dir "%s" %s`, executable, dataDir, command), nil
}
