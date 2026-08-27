//go:build windows

package platform

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// RemoveUserPathEntry removes one exact directory from HKCU\Environment\Path
// while preserving order, unrelated entries and the user's casing.
func RemoveUserPathEntry(directory string) error {
	directory = strings.TrimSpace(filepath.Clean(directory))
	if directory == "" || strings.ContainsAny(directory, "\r\n\x00") {
		return errors.New("diretório de PATH inválido")
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("abrir PATH do usuário: %w", err)
	}
	defer key.Close()
	value, _, err := key.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ler PATH do usuário: %w", err)
	}
	parts := strings.Split(value, ";")
	filtered := make([]string, 0, len(parts))
	removed := false
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" && strings.EqualFold(strings.TrimRight(filepath.Clean(trimmed), `\`), strings.TrimRight(directory, `\`)) {
			removed = true
			continue
		}
		if trimmed != "" {
			filtered = append(filtered, part)
		}
	}
	if !removed {
		return nil
	}
	return key.SetStringValue("Path", strings.Join(filtered, ";"))
}
