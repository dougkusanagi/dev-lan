package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

const ServiceName = "DevLAN"

var ErrUnsupported = errors.New("serviço Windows não é suportado neste sistema")

type Options struct {
	Executable string
	DataDir    string
}

type Status struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	StartType string `json:"start_type,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type Manager interface {
	Install(context.Context, Options) error
	Remove(context.Context) error
	Start(context.Context) error
	Stop(context.Context) error
	Status(context.Context) (Status, error)
}

func NewManager() Manager { return newManager() }

func DefaultOptions(dataDir string) (Options, error) {
	executable, err := os.Executable()
	if err != nil {
		return Options{}, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return Options{}, err
	}
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return Options{}, err
	}
	return Options{Executable: executable, DataDir: dataDir}, nil
}
