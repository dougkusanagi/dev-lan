//go:build !windows

package desktop

import (
	"context"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

func Install(_ context.Context, _ string) error {
	return nil
}

func Uninstall(_ context.Context, _ string) error {
	return nil
}

func Status(_ context.Context, _ string) (State, error) {
	return State{
		Installed:       false,
		Version:         domain.CoreVersion,
		CoreVersion:     domain.CoreVersion,
		ProtocolVersion: domain.ProtocolVersion,
		Compatible:      true,
		Details:         "desktop integration only applicable on Windows",
	}, nil
}
