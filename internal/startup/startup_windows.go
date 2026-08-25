//go:build windows

package startup

import (
	"context"
	"errors"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const valueName = "DevLAN"

func Enable(_ context.Context, executable, dataDir string, mode Mode) error {
	command, err := BuildCommand(executable, dataDir, mode)
	if err != nil {
		return err
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, RunKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(valueName, command)
}

func Disable(_ context.Context) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, RunKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()
	err = key.DeleteValue(valueName)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

func Status(_ context.Context) (State, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, RunKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	defer key.Close()
	command, _, err := key.GetStringValue(valueName)
	if errors.Is(err, registry.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	mode := ModeGUI
	trimmed := strings.TrimSpace(command)
	if strings.HasSuffix(trimmed, " api serve") || strings.HasSuffix(trimmed, " "+string(ModeService)) {
		mode = ModeService
	}
	return State{Enabled: true, Mode: mode, Command: command}, nil
}
