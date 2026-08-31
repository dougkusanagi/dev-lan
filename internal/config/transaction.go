package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

const (
	ConfigSchemaVersion = 1
	StateSchemaVersion  = 1
	ManifestVersion     = 1
	lockRetryInterval   = 25 * time.Millisecond
	lockStaleAfter      = 30 * time.Minute
)

var (
	ErrRevisionConflict = errors.New("revisão de configuração desatualizada")
	ErrNoPreviousState  = errors.New("nenhuma revisão anterior disponível")
)

type persistenceManifest struct {
	Version        int    `json:"version"`
	Revision       uint64 `json:"revision"`
	Status         string `json:"status"`
	ConfigSHA256   string `json:"config_sha256"`
	StateSHA256    string `json:"state_sha256"`
	PreviousConfig bool   `json:"previous_config"`
	PreviousState  bool   `json:"previous_state"`
	UpdatedAt      string `json:"updated_at"`
}

type journalEntry struct {
	At        string `json:"at"`
	Operation string `json:"operation"`
	Revision  uint64 `json:"revision"`
	Phase     string `json:"phase"`
	Error     string `json:"error,omitempty"`
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Store) inject(point string) error {
	if s.Fault == nil {
		return nil
	}
	if err := s.Fault(point); err != nil {
		return fmt.Errorf("falha injetada em %s: %w", point, err)
	}
	return nil
}

// WithLock serializes all state transitions across CLI, API, Wails and
// service processes. O_EXCL is used because it is supported by both NTFS and
// the WSL-mounted Windows filesystem; stale locks are only reclaimed after a
// conservative timeout.
func (s Store) WithLock(ctx context.Context, fn func() error) error {
	fs := s.filesystem()
	if err := fs.MkdirAll(s.Paths().Dir, 0o755); err != nil {
		return err
	}
	file, err := s.acquireLock(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close(); _ = fs.Remove(s.Paths().Lock) }()
	return fn()
}

func (s Store) acquireLock(ctx context.Context) (*os.File, error) {
	path := s.Paths().Lock
	for {
		file, err := s.filesystem().OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, _ = file.WriteString(strconv.Itoa(os.Getpid()) + "\n" + s.now().UTC().Format(time.RFC3339Nano) + "\n")
			_ = file.Sync()
			return file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("criar lock do DevLAN: %w", err)
		}
		if info, statErr := s.filesystem().Stat(path); statErr == nil {
			stale, known := s.lockIsOrphaned(path)
			if (known && stale) || (!known && s.now().Sub(info.ModTime()) > lockStaleAfter) {
				// Reclaim only this exact managed lock. A concurrent owner may
				// win the race; the next loop then observes its new lock.
				_ = s.filesystem().Remove(path)
				continue
			}
		}
		timer := time.NewTimer(lockRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("aguardando lock do DevLAN: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// lockIsOrphaned returns (orphaned, known). Old lock files contain a PID and
// timestamp on separate lines, so malformed/legacy files remain protected by
// the age-based fallback in acquireLock.
func (s Store) lockIsOrphaned(path string) (bool, bool) {
	data, err := s.filesystem().ReadFile(path)
	if err != nil {
		return false, false
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return false, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || pid <= 0 {
		return false, false
	}
	if processAlive(pid) {
		return false, true
	}
	return true, true
}

func (s Store) saveTransaction(cfg domain.Config) error {
	paths := s.Paths()
	if cfg.Version > ConfigSchemaVersion {
		return fmt.Errorf("schema da configuração não suportado: %d", cfg.Version)
	}
	currentRevision, err := s.readRevision()
	if err != nil {
		return err
	}
	if cfg.Revision != 0 && cfg.Revision != currentRevision {
		return fmt.Errorf("%w: esperado %d, atual %d", ErrRevisionConflict, cfg.Revision, currentRevision)
	}
	cfg.Revision = currentRevision + 1
	state := stateFile{
		Version:              cfg.Version,
		SchemaVersion:        StateSchemaVersion,
		Revision:             cfg.Revision,
		Allowlist:            cfg.Allowlist,
		AuthUsers:            cfg.AuthUsers,
		Projects:             cfg.Projects,
		Parks:                cfg.Parks,
		PHPVersions:          cfg.PHPVersions,
		RoutePortAllocations: cfg.RoutePortAllocations,
	}
	stateData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar estado: %w", err)
	}
	stateData = append(stateData, '\n')
	configData := []byte(renderTOMLConfig(cfg))

	oldConfig, configExists, err := readOptionalFS(s.filesystem(), paths.Config)
	if err != nil {
		return err
	}
	oldState, stateExists, err := readOptionalFS(s.filesystem(), paths.State)
	if err != nil {
		return err
	}
	manifest := persistenceManifest{
		Version: ManifestVersion, Revision: cfg.Revision, Status: "prepared",
		ConfigSHA256: digest(configData), StateSHA256: digest(stateData),
		PreviousConfig: configExists, PreviousState: stateExists,
		UpdatedAt: s.now().UTC().Format(time.RFC3339Nano),
	}
	if err := s.appendJournal(journalEntry{At: manifest.UpdatedAt, Operation: "save", Revision: cfg.Revision, Phase: "prepare"}); err != nil {
		return err
	}

	if configExists {
		if err := s.writeAtomicPoint(paths.PreviousConfig, oldConfig, 0o644, "write.previous.config"); err != nil {
			return err
		}
	}
	if stateExists {
		if err := s.writeAtomicPoint(paths.PreviousState, oldState, 0o644, "write.previous.state"); err != nil {
			return err
		}
	}
	if err := s.writeManifest(manifest); err != nil {
		return err
	}

	configTemp, err := writeTempFS(s.filesystem(), paths.Dir, ".devlan-config-{revision}-", configData)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(configTemp) }()
	stateTemp, err := writeTempFS(s.filesystem(), paths.Dir, ".devlan-state-{revision}-", stateData)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(stateTemp) }()
	if err := s.inject("rename.config"); err != nil {
		return err
	}
	if err := replaceFileFS(s.filesystem(), configTemp, paths.Config); err != nil {
		return fmt.Errorf("gravar %s: %w", paths.Config, err)
	}
	if err := s.inject("rename.state"); err != nil {
		return err
	}
	if err := replaceFileFS(s.filesystem(), stateTemp, paths.State); err != nil {
		return fmt.Errorf("gravar %s: %w", paths.State, err)
	}
	if err := s.verifyPair(manifest); err != nil {
		return err
	}
	manifest.Status = "committed"
	if err := s.inject("commit.manifest"); err != nil {
		return err
	}
	if err := s.writeManifest(manifest); err != nil {
		return err
	}
	// The manifest is the commit point. A journal write after it is useful for
	// auditability, but cannot turn a durable commit into a failed transaction.
	// Recovery can always reconstruct the committed pair from the manifest.
	_ = s.appendJournal(journalEntry{At: s.now().UTC().Format(time.RFC3339Nano), Operation: "save", Revision: cfg.Revision, Phase: "finalize"})
	return nil
}

func (s Store) readRevision() (uint64, error) {
	data, err := s.filesystem().ReadFile(s.Paths().State)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("ler revisão persistida: %w", err)
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return 0, fmt.Errorf("ler revisão persistida: %w", err)
	}
	return state.Revision, nil
}

func (s Store) writeManifest(manifest persistenceManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := s.inject("write.manifest"); err != nil {
		return err
	}
	return atomicWriteFS(s.filesystem(), s.Paths().Manifest, data, 0o644)
}

func (s Store) appendJournal(entry journalEntry) error {
	if err := s.inject("write.journal"); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	file, err := s.filesystem().OpenFile(s.Paths().Journal, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s Store) verifyPair(manifest persistenceManifest) error {
	config, err := s.filesystem().ReadFile(s.Paths().Config)
	if err != nil {
		return err
	}
	state, err := s.filesystem().ReadFile(s.Paths().State)
	if err != nil {
		return err
	}
	if digest(config) != manifest.ConfigSHA256 || digest(state) != manifest.StateSHA256 {
		return errors.New("config.toml e state.json não pertencem à mesma revisão")
	}
	return nil
}

func (s Store) recoverTransaction() error {
	data, err := s.filesystem().ReadFile(s.Paths().Manifest)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var manifest persistenceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("ler manifesto de persistência: %w", err)
	}
	if manifest.Version != ManifestVersion {
		return fmt.Errorf("versão do manifesto não suportada: %d", manifest.Version)
	}
	if manifest.Status == "committed" {
		if err := s.verifyPair(manifest); err == nil {
			return nil
		}
	}
	// A transação preparada, ou um manifesto comprometido com arquivos
	// corrompidos, volta ao último par conhecido. Isso também cobre término
	// entre os dois renames.
	if !manifest.PreviousConfig {
		if err := s.filesystem().Remove(s.Paths().Config); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		previous, err := s.filesystem().ReadFile(s.Paths().PreviousConfig)
		if err != nil {
			return err
		}
		if err := atomicWriteFS(s.filesystem(), s.Paths().Config, previous, 0o644); err != nil {
			return err
		}
	}
	if !manifest.PreviousState {
		if err := s.filesystem().Remove(s.Paths().State); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		previous, err := s.filesystem().ReadFile(s.Paths().PreviousState)
		if err != nil {
			return err
		}
		if err := atomicWriteFS(s.filesystem(), s.Paths().State, previous, 0o644); err != nil {
			return err
		}
	}
	_ = s.appendJournal(journalEntry{At: s.now().UTC().Format(time.RFC3339Nano), Operation: "recover", Revision: manifest.Revision, Phase: "rollback"})
	return os.Remove(s.Paths().Manifest)
}

func (s Store) writeAtomicPoint(path string, data []byte, mode os.FileMode, point string) error {
	if err := s.inject(point); err != nil {
		return err
	}
	return atomicWriteFS(s.filesystem(), path, data, mode)
}

func readOptionalFS(fs FileSystem, file string) ([]byte, bool, error) {
	data, err := fs.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func writeTempFS(fs FileSystem, dir, prefix string, data []byte) (string, error) {
	file, err := fs.CreateTemp(dir, prefix)
	if err != nil {
		return "", err
	}
	name := file.Name()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = fs.Remove(name)
		return "", err
	}
	if err := file.Chmod(0o644); err != nil {
		_ = file.Close()
		_ = fs.Remove(name)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = fs.Remove(name)
		return "", err
	}
	return name, nil
}

func atomicWriteFS(fs FileSystem, file string, data []byte, mode os.FileMode) error {
	if err := fs.MkdirAll(filepathDir(file), 0o755); err != nil {
		return err
	}
	temporary, err := writeTempFS(fs, filepathDir(file), filepathBase(file)+"-", data)
	if err != nil {
		return err
	}
	if err := replaceFileFS(fs, temporary, file); err != nil {
		_ = fs.Remove(temporary)
		return err
	}
	return os.Chmod(file, mode)
}

// Small wrappers keep the filesystem seam independent from filepath on tests.
// They intentionally accept only paths already derived from Store.Paths().
func filepathDir(path string) string  { return filepath.Dir(path) }
func filepathBase(path string) string { return filepath.Base(path) }

func replaceFileFS(fs FileSystem, source, target string) error {
	err := fs.Rename(source, target)
	if err == nil {
		return nil
	}
	if removeErr := fs.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return err
	}
	return fs.Rename(source, target)
}

func restoreFS(fs FileSystem, file string, data []byte, existed bool) error {
	if !existed {
		if err := fs.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return atomicWriteFS(fs, file, data, 0o644)
}

func (s Store) RollbackConfig() error {
	return s.WithLock(context.Background(), s.RollbackConfigLocked)
}

func (s Store) RollbackConfigLocked() error {
	paths := s.Paths()
	config, configExists, err := readOptionalFS(s.filesystem(), paths.PreviousConfig)
	if err != nil {
		return err
	}
	state, stateExists, err := readOptionalFS(s.filesystem(), paths.PreviousState)
	if err != nil {
		return err
	}
	if !configExists && !stateExists {
		return ErrNoPreviousState
	}
	if err := restoreFS(s.filesystem(), paths.Config, config, configExists); err != nil {
		return err
	}
	if err := restoreFS(s.filesystem(), paths.State, state, stateExists); err != nil {
		return err
	}
	if configExists && stateExists {
		manifest := persistenceManifest{Version: ManifestVersion, Status: "committed", ConfigSHA256: digest(config), StateSHA256: digest(state), UpdatedAt: s.now().UTC().Format(time.RFC3339Nano)}
		if revision, revErr := revisionFromState(state); revErr == nil {
			manifest.Revision = revision
		}
		if err := s.writeManifest(manifest); err != nil {
			return err
		}
	}
	return nil
}

func revisionFromState(data []byte) (uint64, error) {
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return 0, err
	}
	return state.Revision, nil
}
