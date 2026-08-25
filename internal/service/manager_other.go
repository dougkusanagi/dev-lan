//go:build !windows

package service

import "context"

type unsupportedManager struct{}

func newManager() Manager { return unsupportedManager{} }

func (unsupportedManager) Install(context.Context, Options) error { return ErrUnsupported }
func (unsupportedManager) Remove(context.Context) error           { return ErrUnsupported }
func (unsupportedManager) Start(context.Context) error            { return ErrUnsupported }
func (unsupportedManager) Stop(context.Context) error             { return ErrUnsupported }
func (unsupportedManager) Status(context.Context) (Status, error) {
	return Status{Detail: ErrUnsupported.Error()}, ErrUnsupported
}

func Run(context.Context, string) error { return ErrUnsupported }
