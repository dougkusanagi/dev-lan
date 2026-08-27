//go:build !windows

package platform

import (
	"context"
	"errors"
)

func RemoveCARoot(context.Context, string, ...Runner) error {
	return errors.New("trust store do Windows só é suportado no Windows")
}
