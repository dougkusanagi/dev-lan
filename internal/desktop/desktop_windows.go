//go:build windows

package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

func getShortcutPath() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	programsDir := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs")
	return filepath.Join(programsDir, "DevLAN Dashboard.url"), nil
}

func Install(_ context.Context, _ string) error {
	shortcutPath, err := getShortcutPath()
	if err != nil {
		return fmt.Errorf("determinar caminho de atalho: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(shortcutPath), 0o755); err != nil {
		return fmt.Errorf("criar diretório de atalhos: %w", err)
	}

	content := fmt.Sprintf("[InternetShortcut]\nURL=https://devlan.localhost/\nIconIndex=0\n")
	if err := os.WriteFile(shortcutPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("gravar atalho do DevLAN: %w", err)
	}
	return nil
}

func Uninstall(_ context.Context, _ string) error {
	shortcutPath, err := getShortcutPath()
	if err != nil {
		return err
	}
	if err := os.Remove(shortcutPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remover atalho do DevLAN: %w", err)
	}
	return nil
}

func Status(_ context.Context, _ string) (State, error) {
	shortcutPath, err := getShortcutPath()
	if err != nil {
		return State{}, err
	}
	installed := false
	if _, err := os.Stat(shortcutPath); err == nil {
		installed = true
	}
	return State{
		Installed:       installed,
		Version:         domain.CoreVersion,
		CoreVersion:     domain.CoreVersion,
		ProtocolVersion: domain.ProtocolVersion,
		Compatible:      true,
		ShortcutPath:    shortcutPath,
		Details:         "Atalho do Menu Iniciar",
	}, nil
}
