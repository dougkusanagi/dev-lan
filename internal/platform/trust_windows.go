//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// RemoveCARoot removes only the exact certificate identified by its SHA-1
// thumbprint (the selector used by certutil) from the current user's Root
// store. It never clears the store.
func RemoveCARoot(ctx context.Context, fingerprint string, runners ...Runner) error {
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" || strings.ContainsAny(fingerprint, "\r\n\t ") {
		return errors.New("fingerprint da CA inválido")
	}
	var runner Runner
	if len(runners) > 0 && runners[0] != nil {
		runner = runners[0]
	} else {
		runner = NewExecRunner("certutil.exe")
	}
	if _, err := runner.Run(ctx, "-user", "-delstore", "Root", fingerprint); err != nil {
		return fmt.Errorf("remover CA raiz do trust store do usuário: %w", err)
	}
	return nil
}
