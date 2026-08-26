package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// Store owns only DevLAN-managed files. Project directories are never inside
// this store and are therefore untouched by uninstall or rollback.
type Store struct {
	Dir string
	// FS is the host-I/O seam used by persistence and fault-injection tests.
	FS FileSystem
	// Fault is intentionally injectable so transaction recovery can be tested
	// at every write/rename boundary without corrupting a real installation.
	Fault FaultInjector
	// Now is injected by tests and keeps audit/transaction timestamps
	// deterministic.
	Now func() time.Time
}

type FaultInjector func(point string) error

// FileSystem is deliberately small: persistence does not need to know about
// processes, shells or project files. The production implementation delegates
// to os and tests may provide a faulting implementation.
type FileSystem interface {
	OpenFile(name string, flag int, perm os.FileMode) (*os.File, error)
	ReadFile(name string) ([]byte, error)
	Stat(name string) (os.FileInfo, error)
	Remove(name string) error
	MkdirAll(path string, perm os.FileMode) error
	CreateTemp(dir, pattern string) (*os.File, error)
	Rename(oldPath, newPath string) error
}

type OSFileSystem struct{}

func (OSFileSystem) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}
func (OSFileSystem) ReadFile(name string) ([]byte, error)         { return os.ReadFile(name) }
func (OSFileSystem) Stat(name string) (os.FileInfo, error)        { return os.Stat(name) }
func (OSFileSystem) Remove(name string) error                     { return os.Remove(name) }
func (OSFileSystem) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (OSFileSystem) CreateTemp(dir, pattern string) (*os.File, error) {
	return os.CreateTemp(dir, pattern)
}
func (OSFileSystem) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }

type Paths struct {
	Dir             string
	Config          string
	State           string
	APIToken        string
	APIEndpoint     string
	Telemetry       string
	TelemetryQueue  string
	CARootExport    string
	GeneratedDir    string
	PHPGeneratedDir string
	PHPPreviousDir  string
	PHPInfoDir      string
	WindowsCaddy    string
	WSLCaddy        string
	WindowsPrevious string
	WSLPrevious     string
	LogsDir         string
	SecurityLog     string
	Lock            string
	Manifest        string
	Journal         string
	PreviousConfig  string
	PreviousState   string
}

func NewStore(dir string) Store { return Store{Dir: dir, FS: OSFileSystem{}, Now: time.Now} }

func (s Store) filesystem() FileSystem {
	if s.FS != nil {
		return s.FS
	}
	return OSFileSystem{}
}

func (s Store) Paths() Paths {
	generated := filepath.Join(s.Dir, "generated")
	return Paths{
		Dir:             s.Dir,
		Config:          filepath.Join(s.Dir, "config.toml"),
		State:           filepath.Join(s.Dir, "state.json"),
		APIToken:        filepath.Join(s.Dir, "api.token"),
		APIEndpoint:     filepath.Join(s.Dir, "api.endpoint.json"),
		Telemetry:       filepath.Join(s.Dir, "telemetry.json"),
		TelemetryQueue:  filepath.Join(s.Dir, "telemetry.queue.jsonl"),
		CARootExport:    filepath.Join(s.Dir, "devlan-ca-root.crt"),
		GeneratedDir:    generated,
		PHPGeneratedDir: filepath.Join(generated, "php"),
		PHPPreviousDir:  filepath.Join(generated, "php.previous"),
		PHPInfoDir:      filepath.Join(generated, "php", "info"),
		WindowsCaddy:    filepath.Join(generated, "Caddyfile.windows"),
		WSLCaddy:        filepath.Join(generated, "Caddyfile.wsl"),
		WindowsPrevious: filepath.Join(generated, "Caddyfile.windows.previous"),
		WSLPrevious:     filepath.Join(generated, "Caddyfile.wsl.previous"),
		LogsDir:         filepath.Join(s.Dir, "logs"),
		SecurityLog:     filepath.Join(s.Dir, "logs", "security.log"),
		Lock:            filepath.Join(s.Dir, ".lock"),
		Manifest:        filepath.Join(s.Dir, "manifest.json"),
		Journal:         filepath.Join(s.Dir, "journal.jsonl"),
		PreviousConfig:  filepath.Join(s.Dir, "config.toml.previous"),
		PreviousState:   filepath.Join(s.Dir, "state.json.previous"),
	}
}

func (s Store) Ensure() error {
	fs := s.filesystem()
	paths := s.Paths()
	for _, dir := range []string{paths.Dir, paths.GeneratedDir, paths.PHPGeneratedDir, paths.PHPInfoDir, paths.LogsDir} {
		if err := fs.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("criar diretório %s: %w", dir, err)
		}
	}
	return nil
}

type stateFile struct {
	Version       int                       `json:"version"`
	SchemaVersion int                       `json:"schema_version,omitempty"`
	Revision      uint64                    `json:"revision"`
	Allowlist     []string                  `json:"allowlist,omitempty"`
	AuthUsers     []domain.AuthUser         `json:"auth_users,omitempty"`
	Projects      []domain.Project          `json:"projects"`
	Parks         []domain.Park             `json:"parks"`
	PHPVersions   []domain.PHPVersionConfig `json:"php_versions,omitempty"`
}

func (s Store) Load() (domain.Config, error) {
	var cfg domain.Config
	err := s.WithLock(context.Background(), func() error {
		var loadErr error
		cfg, loadErr = s.LoadLocked()
		return loadErr
	})
	return cfg, err
}

// LoadLocked is the read side of the persistence transaction. The caller
// must hold Store.WithLock; it is public so the application coordinator can
// compare a revision and commit under one lock.
func (s Store) LoadLocked() (domain.Config, error) {
	if err := s.recoverTransaction(); err != nil {
		return domain.Config{}, err
	}
	cfg := domain.NewConfig()
	paths := s.Paths()

	if data, err := s.filesystem().ReadFile(paths.Config); err == nil {
		if err := parseTOMLConfig(data, &cfg); err != nil {
			return domain.Config{}, fmt.Errorf("ler %s: %w", paths.Config, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return domain.Config{}, fmt.Errorf("ler %s: %w", paths.Config, err)
	}

	if data, err := s.filesystem().ReadFile(paths.State); err == nil {
		var state stateFile
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&state); err != nil {
			return domain.Config{}, fmt.Errorf("ler %s: %w", paths.State, err)
		}
		if state.SchemaVersion == 0 {
			state.SchemaVersion = 1
		}
		if state.SchemaVersion > StateSchemaVersion {
			return domain.Config{}, fmt.Errorf("schema do estado não suportado: %d", state.SchemaVersion)
		}
		cfg.Version = state.Version
		cfg.Revision = state.Revision
		cfg.Projects = state.Projects
		cfg.Parks = state.Parks
		cfg.PHPVersions = state.PHPVersions
		if len(state.Allowlist) > 0 {
			cfg.Allowlist = state.Allowlist
		}
		if len(state.AuthUsers) > 0 {
			cfg.AuthUsers = state.AuthUsers
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return domain.Config{}, fmt.Errorf("ler %s: %w", paths.State, err)
	}

	if err := cfg.Normalize(); err != nil {
		return domain.Config{}, fmt.Errorf("validar configuração: %w", err)
	}
	return cfg, nil
}

func (s Store) Save(cfg domain.Config) error {
	return s.WithLock(context.Background(), func() error { return s.SaveLocked(cfg) })
}

// SaveLocked persists config and state as one recoverable transaction. The
// caller must hold Store.WithLock.
func (s Store) SaveLocked(cfg domain.Config) error {
	if err := cfg.Normalize(); err != nil {
		return err
	}
	if err := s.Ensure(); err != nil {
		return err
	}
	return s.saveTransaction(cfg)
}

func (s Store) AppendSecurityAudit(event string, details string) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	stamp := s.now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("[%s] EVENT=%s %s\n", stamp, event, details)
	file, err := s.filesystem().OpenFile(s.Paths().SecurityLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(line)
	return err
}

func (s Store) ReadSecurityAudit(maxLines int) (string, error) {
	data, err := s.filesystem().ReadFile(s.Paths().SecurityLog)
	if errors.Is(err, os.ErrNotExist) {
		return "(nenhum log de segurança registrado)\n", nil
	}
	if err != nil {
		return "", err
	}
	if maxLines <= 0 {
		return string(data), nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func renderTOMLConfig(cfg domain.Config) string {
	var b strings.Builder
	b.WriteString("# DevLAN configuration. Managed by the CLI.\n")
	fmt.Fprintf(&b, "version = %d\n", cfg.Version)
	fmt.Fprintf(&b, "default_mode = %s\n", strconv.Quote(string(cfg.DefaultMode)))
	fmt.Fprintf(&b, "route_base_port = %d\n", cfg.RouteBasePort)
	fmt.Fprintf(&b, "lan_address = %s\n", strconv.Quote(cfg.LANAddress))
	fmt.Fprintf(&b, "windows_port = %d\n", cfg.WindowsPort)
	fmt.Fprintf(&b, "https_port = %d\n", cfg.HTTPSPort)
	fmt.Fprintf(&b, "tls_enabled = %t\n", cfg.TLSEnabled)
	fmt.Fprintf(&b, "wsl_port = %d\n", cfg.WSLPort)
	fmt.Fprintf(&b, "php_fpm_socket = %s\n", strconv.Quote(cfg.PHPFPMOsocket))
	fmt.Fprintf(&b, "php_default_version = %s\n", strconv.Quote(cfg.PHPDefaultVersion))
	fmt.Fprintf(&b, "php_pool_max_children = %d\n", cfg.PHPFPMPool.MaxChildren)
	fmt.Fprintf(&b, "php_pool_idle_timeout = %s\n", strconv.Quote(cfg.PHPFPMPool.IdleTimeout))
	fmt.Fprintf(&b, "php_pool_max_requests = %d\n", cfg.PHPFPMPool.MaxRequests)
	fmt.Fprintf(&b, "composer_environment = %s\n", strconv.Quote(string(cfg.Composer.Environment)))
	fmt.Fprintf(&b, "composer_binary = %s\n", strconv.Quote(cfg.Composer.Binary))
	if len(cfg.Allowlist) > 0 {
		quoted := make([]string, len(cfg.Allowlist))
		for i, a := range cfg.Allowlist {
			quoted[i] = strconv.Quote(a)
		}
		fmt.Fprintf(&b, "allowlist = [%s]\n", strings.Join(quoted, ", "))
	}
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
			if parsed > ConfigSchemaVersion {
				return fmt.Errorf("linha %d: schema da configuração não suportado: %d", lineNumber+1, parsed)
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
		case "route_base_port":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("linha %d: route_base_port inválida", lineNumber+1)
			}
			cfg.RouteBasePort = parsed
		case "allowlist":
			list, err := parseTOMLStringList(value)
			if err != nil {
				return fmt.Errorf("linha %d: allowlist inválida: %w", lineNumber+1, err)
			}
			cfg.Allowlist = list
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
		case "php_default_version":
			parsed, err := parseTOMLString(value)
			if err != nil {
				return fmt.Errorf("linha %d: php_default_version inválida: %w", lineNumber+1, err)
			}
			cfg.PHPDefaultVersion = parsed
		case "php_pool_max_children":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("linha %d: php_pool_max_children inválido", lineNumber+1)
			}
			cfg.PHPFPMPool.MaxChildren = parsed
		case "php_pool_idle_timeout":
			parsed, err := parseTOMLString(value)
			if err != nil {
				return fmt.Errorf("linha %d: php_pool_idle_timeout inválido: %w", lineNumber+1, err)
			}
			cfg.PHPFPMPool.IdleTimeout = parsed
		case "php_pool_max_requests":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("linha %d: php_pool_max_requests inválido", lineNumber+1)
			}
			cfg.PHPFPMPool.MaxRequests = parsed
		case "composer_environment":
			parsed, err := parseTOMLString(value)
			if err != nil {
				return fmt.Errorf("linha %d: composer_environment inválido: %w", lineNumber+1, err)
			}
			cfg.Composer.Environment = domain.ComposerEnvironment(parsed)
		case "composer_binary":
			parsed, err := parseTOMLString(value)
			if err != nil {
				return fmt.Errorf("linha %d: composer_binary inválido: %w", lineNumber+1, err)
			}
			cfg.Composer.Binary = parsed
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

func parseTOMLStringList(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		value = value[1 : len(value)-1]
	}
	if strings.TrimSpace(value) == "" {
		return []string{}, nil
	}
	parts := strings.Split(value, ",")
	res := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		unquoted, err := parseTOMLString(p)
		if err != nil {
			unquoted = p
		}
		res = append(res, unquoted)
	}
	return res, nil
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

// ApplyPHPFiles replaces only the generated PHP-FPM configuration and the
// sanitized information page. All paths are derived inside the managed data
// directory; callers provide filenames rather than arbitrary paths.
func (s Store) ApplyPHPFiles(poolFiles map[string]string, infoPage string) error {
	if err := s.Ensure(); err != nil {
		return err
	}
	paths := s.Paths()
	if err := os.RemoveAll(paths.PHPPreviousDir); err != nil {
		return fmt.Errorf("preparar rollback dos pools PHP: %w", err)
	}
	if err := os.MkdirAll(paths.PHPPreviousDir, 0o755); err != nil {
		return fmt.Errorf("criar rollback dos pools PHP: %w", err)
	}
	entries, readErr := os.ReadDir(paths.PHPGeneratedDir)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if entry.Name() != "info" {
				continue
			}
			source := filepath.Join(paths.PHPGeneratedDir, entry.Name())
			target := filepath.Join(paths.PHPPreviousDir, entry.Name())
			if err := copyManagedTree(source, target); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		source := filepath.Join(paths.PHPGeneratedDir, entry.Name())
		target := filepath.Join(paths.PHPPreviousDir, entry.Name())
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		if err := atomicWrite(target, data, 0o644); err != nil {
			return err
		}
	}
	for name, contents := range poolFiles {
		if filepath.Base(name) != name || !strings.HasSuffix(name, ".conf") {
			return fmt.Errorf("arquivo de pool PHP inválido: %q", name)
		}
		if err := atomicWrite(filepath.Join(paths.PHPGeneratedDir, name), []byte(contents), 0o644); err != nil {
			return fmt.Errorf("gravar pool PHP %s: %w", name, err)
		}
	}
	entries, err := os.ReadDir(paths.PHPGeneratedDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		if _, keep := poolFiles[entry.Name()]; !keep {
			if err := os.Remove(filepath.Join(paths.PHPGeneratedDir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remover pool PHP antigo %s: %w", entry.Name(), err)
			}
		}
	}
	if infoPage != "" {
		if err := atomicWrite(filepath.Join(paths.PHPInfoDir, "index.html"), []byte(infoPage), 0o644); err != nil {
			return fmt.Errorf("gravar página de informações PHP: %w", err)
		}
	}
	return nil
}

func (s Store) RollbackPHPFiles() error {
	paths := s.Paths()
	if err := os.RemoveAll(paths.PHPGeneratedDir); err != nil {
		return err
	}
	previous, err := os.ReadDir(paths.PHPPreviousDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.PHPGeneratedDir, 0o755); err != nil {
		return err
	}
	for _, entry := range previous {
		source := filepath.Join(paths.PHPPreviousDir, entry.Name())
		target := filepath.Join(paths.PHPGeneratedDir, entry.Name())
		if entry.IsDir() {
			if err := copyManagedTree(source, target); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		if err := atomicWrite(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func copyManagedTree(source, target string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(source, entry.Name())
		targetPath := filepath.Join(target, entry.Name())
		if entry.IsDir() {
			if err := copyManagedTree(sourcePath, targetPath); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		if err := atomicWrite(targetPath, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) RemoveManagedFiles() error {
	return s.WithLock(context.Background(), s.RemoveManagedFilesLocked)
}

func (s Store) RemoveManagedFilesLocked() error {
	paths := s.Paths()
	files := []string{paths.Config, paths.State, paths.Manifest, paths.Journal, paths.PreviousConfig, paths.PreviousState, paths.APIToken, paths.APIEndpoint, paths.Telemetry, paths.TelemetryQueue, paths.CARootExport}
	for _, file := range files {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remover %s: %w", file, err)
		}
	}
	if err := os.RemoveAll(paths.GeneratedDir); err != nil {
		return fmt.Errorf("remover arquivos gerados: %w", err)
	}
	if err := os.RemoveAll(paths.LogsDir); err != nil {
		return fmt.Errorf("remover logs gerenciados: %w", err)
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
	versions := append([]domain.PHPVersionConfig(nil), cfg.PHPVersions...)
	sort.Slice(versions, func(i, j int) bool { return versions[i].Version < versions[j].Version })
	state := stateFile{Version: cfg.Version, SchemaVersion: StateSchemaVersion, Revision: cfg.Revision, Projects: projects, Parks: parks, PHPVersions: versions}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, err
	}
	return bytes.TrimSpace(data), nil
}
