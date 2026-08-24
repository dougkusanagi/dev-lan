package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// Store owns only DevLAN-managed files. Project directories are never inside
// this store and are therefore untouched by uninstall or rollback.
type Store struct {
	Dir string
}

type Paths struct {
	Dir             string
	Config          string
	State           string
	GeneratedDir    string
	WindowsCaddy    string
	WSLCaddy        string
	WindowsPrevious string
	WSLPrevious     string
	LogsDir         string
}

func NewStore(dir string) Store { return Store{Dir: dir} }

func (s Store) Paths() Paths {
	generated := filepath.Join(s.Dir, "generated")
	return Paths{
		Dir:             s.Dir,
		Config:          filepath.Join(s.Dir, "config.toml"),
		State:           filepath.Join(s.Dir, "state.json"),
		GeneratedDir:    generated,
		WindowsCaddy:    filepath.Join(generated, "Caddyfile.windows"),
		WSLCaddy:        filepath.Join(generated, "Caddyfile.wsl"),
		WindowsPrevious: filepath.Join(generated, "Caddyfile.windows.previous"),
		WSLPrevious:     filepath.Join(generated, "Caddyfile.wsl.previous"),
		LogsDir:         filepath.Join(s.Dir, "logs"),
	}
}

func (s Store) Ensure() error {
	paths := s.Paths()
	for _, dir := range []string{paths.Dir, paths.GeneratedDir, paths.LogsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("criar diretório %s: %w", dir, err)
		}
	}
	return nil
}

type stateFile struct {
	Version  int              `json:"version"`
	Projects []domain.Project `json:"projects"`
	Parks    []domain.Park    `json:"parks"`
}

func (s Store) Load() (domain.Config, error) {
	cfg := domain.NewConfig()
	paths := s.Paths()

	if data, err := os.ReadFile(paths.Config); err == nil {
		if err := parseTOMLConfig(data, &cfg); err != nil {
			return domain.Config{}, fmt.Errorf("ler %s: %w", paths.Config, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return domain.Config{}, fmt.Errorf("ler %s: %w", paths.Config, err)
	}

	if data, err := os.ReadFile(paths.State); err == nil {
		var state stateFile
		if err := json.Unmarshal(data, &state); err != nil {
			return domain.Config{}, fmt.Errorf("ler %s: %w", paths.State, err)
		}
		cfg.Version = state.Version
		cfg.Projects = state.Projects
		cfg.Parks = state.Parks
	} else if !errors.Is(err, os.ErrNotExist) {
		return domain.Config{}, fmt.Errorf("ler %s: %w", paths.State, err)
	}

	if err := cfg.Normalize(); err != nil {
		return domain.Config{}, fmt.Errorf("validar configuração: %w", err)
	}
	return cfg, nil
}

func (s Store) Save(cfg domain.Config) error {
	if err := cfg.Normalize(); err != nil {
		return err
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	paths := s.Paths()
	if err := atomicWrite(paths.Config, []byte(renderTOMLConfig(cfg)), 0o644); err != nil {
		return fmt.Errorf("gravar %s: %w", paths.Config, err)
	}
	state := stateFile{Version: cfg.Version, Projects: cfg.Projects, Parks: cfg.Parks}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar estado: %w", err)
	}
	data = append(data, '\n')
	if err := atomicWrite(paths.State, data, 0o644); err != nil {
		return fmt.Errorf("gravar %s: %w", paths.State, err)
	}
	return nil
}

func renderTOMLConfig(cfg domain.Config) string {
	var b strings.Builder
	b.WriteString("# DevLAN configuration. Managed by the CLI.\n")
	fmt.Fprintf(&b, "version = %d\n", cfg.Version)
	fmt.Fprintf(&b, "default_mode = %s\n", strconv.Quote(string(cfg.DefaultMode)))
	fmt.Fprintf(&b, "lan_address = %s\n", strconv.Quote(cfg.LANAddress))
	fmt.Fprintf(&b, "windows_port = %d\n", cfg.WindowsPort)
	fmt.Fprintf(&b, "https_port = %d\n", cfg.HTTPSPort)
	fmt.Fprintf(&b, "tls_enabled = %t\n", cfg.TLSEnabled)
	fmt.Fprintf(&b, "wsl_port = %d\n", cfg.WSLPort)
	fmt.Fprintf(&b, "php_fpm_socket = %s\n", strconv.Quote(cfg.PHPFPMOsocket))
	return b.String()
}

func parseTOMLConfig(data []byte, cfg *domain.Config) error {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for lineNumber, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		separator := strings.IndexByte(line, '=')
		if separator < 1 {
			return fmt.Errorf("linha %d: esperado chave = valor", lineNumber+1)
		}
		key := strings.TrimSpace(line[:separator])
		value := strings.TrimSpace(line[separator+1:])
		switch key {
		case "version":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("linha %d: version inválida", lineNumber+1)
			}
			cfg.Version = parsed
		case "windows_port":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("linha %d: windows_port inválida", lineNumber+1)
			}
			cfg.WindowsPort = parsed
		case "wsl_port":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("linha %d: wsl_port inválida", lineNumber+1)
			}
			cfg.WSLPort = parsed
		case "https_port":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("linha %d: https_port inválida", lineNumber+1)
			}
			cfg.HTTPSPort = parsed
		case "tls_enabled":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("linha %d: tls_enabled inválido", lineNumber+1)
			}
			cfg.TLSEnabled = parsed
		case "default_mode":
			parsed, err := parseTOMLString(value)
			if err != nil {
				return fmt.Errorf("linha %d: default_mode inválido: %w", lineNumber+1, err)
			}
			mode, err := domain.ParseMode(parsed)
			if err != nil {
				return fmt.Errorf("linha %d: %w", lineNumber+1, err)
			}
			cfg.DefaultMode = mode
		case "lan_address":
			parsed, err := parseTOMLString(value)
			if err != nil {
				return fmt.Errorf("linha %d: lan_address inválido: %w", lineNumber+1, err)
			}
			cfg.LANAddress = parsed
		case "php_fpm_socket":
			parsed, err := parseTOMLString(value)
			if err != nil {
				return fmt.Errorf("linha %d: php_fpm_socket inválido: %w", lineNumber+1, err)
			}
			cfg.PHPFPMOsocket = parsed
		default:
			return fmt.Errorf("linha %d: chave desconhecida %q", lineNumber+1, key)
		}
	}
	return nil
}

func parseTOMLString(value string) (string, error) {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", fmt.Errorf("esperado texto entre aspas")
	}
	parsed, err := strconv.Unquote(value)
	if err != nil {
		return "", err
	}
	return parsed, nil
}

// ApplyGenerated validates temporary files before replacing the last known
// good pair. The validator receives local temporary paths and may be nil when
// the runtime dependency is not installed yet.
func (s Store) ApplyGenerated(windows, wsl string, validator func(windowsTemp, wslTemp string) error) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	paths := s.Paths()
	temporary := make([]string, 0, 2)
	defer func() {
		for _, file := range temporary {
			_ = os.Remove(file)
		}
	}()

	windowsTemp, err := writeTemp(paths.GeneratedDir, "Caddyfile.windows-", []byte(windows))
	if err != nil {
		return err
	}
	temporary = append(temporary, windowsTemp)
	wslTemp, err := writeTemp(paths.GeneratedDir, "Caddyfile.wsl-", []byte(wsl))
	if err != nil {
		return err
	}
	temporary = append(temporary, wslTemp)
	if validator != nil {
		if err := validator(windowsTemp, wslTemp); err != nil {
			return fmt.Errorf("validação rejeitou a nova configuração: %w", err)
		}
	}

	oldWindows, windowsExists, err := readOptional(paths.WindowsCaddy)
	if err != nil {
		return err
	}
	oldWSL, wslExists, err := readOptional(paths.WSLCaddy)
	if err != nil {
		return err
	}
	if windowsExists {
		if err := atomicWrite(paths.WindowsPrevious, oldWindows, 0o644); err != nil {
			return fmt.Errorf("salvar rollback Windows: %w", err)
		}
	}
	if wslExists {
		if err := atomicWrite(paths.WSLPrevious, oldWSL, 0o644); err != nil {
			return fmt.Errorf("salvar rollback WSL: %w", err)
		}
	}

	if err := replaceFile(windowsTemp, paths.WindowsCaddy); err != nil {
		return fmt.Errorf("aplicar Caddy Windows: %w", err)
	}
	if err := replaceFile(wslTemp, paths.WSLCaddy); err != nil {
		_ = restore(paths.WindowsCaddy, oldWindows, windowsExists)
		return fmt.Errorf("aplicar Caddy WSL: %w", err)
	}
	return nil
}

func (s Store) Generated() (windows, wsl string, err error) {
	paths := s.Paths()
	windowsData, err := os.ReadFile(paths.WindowsCaddy)
	if err != nil {
		return "", "", err
	}
	wslData, err := os.ReadFile(paths.WSLCaddy)
	if err != nil {
		return "", "", err
	}
	return string(windowsData), string(wslData), nil
}

// RollbackGenerated restores the pair saved by the last successful apply. If
// no previous pair exists, the current file is removed, which is the correct
// rollback for the first generated configuration.
func (s Store) RollbackGenerated() error {
	paths := s.Paths()
	previousWindows, windowsExists, err := readOptional(paths.WindowsPrevious)
	if err != nil {
		return err
	}
	previousWSL, wslExists, err := readOptional(paths.WSLPrevious)
	if err != nil {
		return err
	}
	if err := restore(paths.WindowsCaddy, previousWindows, windowsExists); err != nil {
		return fmt.Errorf("rollback Caddy Windows: %w", err)
	}
	if err := restore(paths.WSLCaddy, previousWSL, wslExists); err != nil {
		return fmt.Errorf("rollback Caddy WSL: %w", err)
	}
	return nil
}

func (s Store) RemoveManagedFiles() error {
	paths := s.Paths()
	files := []string{paths.Config, paths.State, paths.WindowsCaddy, paths.WSLCaddy, paths.WindowsPrevious, paths.WSLPrevious}
	for _, file := range files {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remover %s: %w", file, err)
		}
	}
	return nil
}

func readOptional(file string) ([]byte, bool, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func writeTemp(dir, prefix string, data []byte) (string, error) {
	file, err := os.CreateTemp(dir, prefix)
	if err != nil {
		return "", fmt.Errorf("criar temporário em %s: %w", dir, err)
	}
	name := file.Name()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := file.Chmod(0o644); err != nil {
		_ = file.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func atomicWrite(file string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temporary, err := writeTemp(dir, filepath.Base(file)+"-", data)
	if err != nil {
		return err
	}
	if err := replaceFile(temporary, file); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Chmod(file, mode)
}

func replaceFile(source, target string) error {
	// os.Rename is atomic on the same filesystem. Windows refuses replacing an
	// existing file, so remove only this exact managed target before retrying.
	if err := os.Rename(source, target); err == nil {
		return nil
	} else if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, target)
}

func restore(file string, data []byte, existed bool) error {
	if !existed {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return atomicWrite(file, data, 0o644)
}

// StableJSON is useful for diagnostics and tests that want to inspect the
// persisted registry without depending on map iteration order.
func StableJSON(cfg domain.Config) ([]byte, error) {
	projects := append([]domain.Project(nil), cfg.Projects...)
	parks := append([]domain.Park(nil), cfg.Parks...)
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	sort.Slice(parks, func(i, j int) bool { return parks[i].Path < parks[j].Path })
	state := stateFile{Version: cfg.Version, Projects: projects, Parks: parks}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, err
	}
	return bytes.TrimSpace(data), nil
}
