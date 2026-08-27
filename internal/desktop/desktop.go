// Package desktop manages optional desktop integration (shortcuts, tray,
// status and version compatibility) independently from the DevLAN Core.
package desktop

import (
	"context"
	"fmt"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

type State struct {
	Installed       bool   `json:"installed"`
	Version         string `json:"version"`
	CoreVersion     string `json:"core_version"`
	ProtocolVersion int    `json:"protocol_version"`
	Compatible      bool   `json:"compatible"`
	ShortcutPath    string `json:"shortcut_path,omitempty"`
	Details         string `json:"details,omitempty"`
}

func CheckCompatibility(desktopProtocol int, coreProtocol int) (bool, string) {
	if desktopProtocol != coreProtocol {
		return false, fmt.Sprintf("incompatibilidade de protocolo: Desktop v%d != Core v%d; execute `devlan desktop install` para atualizar", desktopProtocol, coreProtocol)
	}
	return true, "compatível"
}

func CurrentState(ctx context.Context, dataDir string) (State, error) {
	state, err := Status(ctx, dataDir)
	if err != nil {
		return State{}, err
	}
	state.CoreVersion = domain.CoreVersion
	state.ProtocolVersion = domain.ProtocolVersion
	compat, details := CheckCompatibility(state.ProtocolVersion, domain.ProtocolVersion)
	state.Compatible = compat
	if state.Details == "" {
		state.Details = details
	}
	return state, nil
}
