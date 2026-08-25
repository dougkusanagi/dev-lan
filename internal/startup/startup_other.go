//go:build !windows

package startup

import "context"

func Enable(context.Context, string, string, Mode) error { return ErrUnsupported }
func Disable(context.Context) error                      { return ErrUnsupported }
func Status(context.Context) (State, error)              { return State{}, ErrUnsupported }
