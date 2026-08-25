package diagnostic

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	Format  = "devlan-diagnostic"
	Version = 1
)

type Manifest struct {
	Format    string `json:"format"`
	Version   int    `json:"version"`
	CreatedAt string `json:"created_at"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Write creates a deterministic, single-file support bundle. Entries are
// supplied by the caller so this package never walks a project directory or
// accidentally packages application secrets.
func Write(target string, manifest Manifest, entries map[string][]byte) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("caminho do diagnóstico não pode ser vazio")
	}
	if manifest.Format == "" {
		manifest.Format = Format
	}
	if manifest.Version == 0 {
		manifest.Version = Version
	}
	if manifest.CreatedAt == "" {
		manifest.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar manifesto do diagnóstico: %w", err)
	}
	if entries == nil {
		entries = map[string][]byte{}
	}
	entries = cloneEntries(entries)
	entries["manifest.json"] = append(manifestData, '\n')

	names := make([]string, 0, len(entries))
	for name := range entries {
		if err := validateEntryName(name); err != nil {
			return err
		}
		names = append(names, name)
	}
	sort.Strings(names)

	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolver destino do diagnóstico: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absoluteTarget), 0o755); err != nil {
		return fmt.Errorf("criar diretório do diagnóstico: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(absoluteTarget), ".devlan-diagnostic-*.tmp")
	if err != nil {
		return fmt.Errorf("criar arquivo temporário do diagnóstico: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryName)
	}()

	archive := zip.NewWriter(temporary)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o600)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			_ = archive.Close()
			_ = temporary.Close()
			return fmt.Errorf("criar entrada %s: %w", name, err)
		}
		if _, err := writer.Write(entries[name]); err != nil {
			_ = archive.Close()
			_ = temporary.Close()
			return fmt.Errorf("gravar entrada %s: %w", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("finalizar diagnóstico: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("fechar diagnóstico: %w", err)
	}
	if err := os.Chmod(temporaryName, 0o600); err != nil {
		return fmt.Errorf("proteger diagnóstico: %w", err)
	}
	if err := replaceFile(temporaryName, absoluteTarget); err != nil {
		return fmt.Errorf("publicar diagnóstico: %w", err)
	}
	return nil
}

func cloneEntries(input map[string][]byte) map[string][]byte {
	output := make(map[string][]byte, len(input))
	for name, data := range input {
		output[name] = append([]byte(nil), data...)
	}
	return output
}

func validateEntryName(name string) error {
	if name == "" || filepath.IsAbs(name) || strings.ContainsAny(name, "\\\x00") {
		return fmt.Errorf("nome de entrada inválido no diagnóstico: %q", name)
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || clean != name || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return fmt.Errorf("nome de entrada inseguro no diagnóstico: %q", name)
	}
	return nil
}

func replaceFile(source, target string) error {
	if err := os.Rename(source, target); err == nil {
		return nil
	} else if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, target)
}
