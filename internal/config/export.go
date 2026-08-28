package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

const (
	// ExportFormat is deliberately explicit so a file from another tool cannot
	// accidentally be imported as DevLAN state.
	ExportFormat  = "devlan-config"
	ExportVersion = 1
)

// ExportBundle is the portable, non-secret representation of the DevLAN
// configuration. Authentication material and temporary network exposure are
// removed by NewExportBundle before the bundle is serialized.
type ExportBundle struct {
	Format  string        `json:"format"`
	Version int           `json:"version"`
	Config  domain.Config `json:"config"`
}

// SanitizeForExport returns a deep copy so exporting never mutates the live
// configuration. Password hashes are credentials too: they are not included
// even though the application never stores the clear-text password.
func SanitizeForExport(input domain.Config) (domain.Config, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return domain.Config{}, fmt.Errorf("copiar configuração para exportação: %w", err)
	}
	var output domain.Config
	if err := json.Unmarshal(data, &output); err != nil {
		return domain.Config{}, fmt.Errorf("copiar configuração para exportação: %w", err)
	}
	output.AuthUsers = nil
	for index := range output.Projects {
		output.Projects[index].AuthEnabled = nil
		output.Projects[index].AuthUsers = nil
		// An exposure is intentionally a local, expiring runtime decision and
		// must not be resurrected on another machine by an import.
		output.Projects[index].ExposedUntil = nil
	}
	if err := output.Normalize(); err != nil {
		return domain.Config{}, fmt.Errorf("validar configuração exportada: %w", err)
	}
	return output, nil
}

func NewExportBundle(input domain.Config) (ExportBundle, error) {
	output, err := SanitizeForExport(input)
	if err != nil {
		return ExportBundle{}, err
	}
	return ExportBundle{Format: ExportFormat, Version: ExportVersion, Config: output}, nil
}

func MarshalExport(input domain.Config) ([]byte, error) {
	bundle, err := NewExportBundle(input)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("serializar exportação: %w", err)
	}
	return append(data, '\n'), nil
}

// UnmarshalExport validates the envelope and strips sensitive fields again.
// The second sanitization makes importing a hand-edited or older file safe.
func UnmarshalExport(data []byte) (domain.Config, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var bundle ExportBundle
	if err := decoder.Decode(&bundle); err != nil {
		return domain.Config{}, fmt.Errorf("ler arquivo de configuração exportado: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return domain.Config{}, fmt.Errorf("arquivo de configuração exportado contém dados após o JSON principal")
		}
		return domain.Config{}, fmt.Errorf("dados extras no arquivo de configuração exportado: %w", err)
	}
	if bundle.Format != ExportFormat {
		return domain.Config{}, fmt.Errorf("formato de exportação inválido: %q", bundle.Format)
	}
	if bundle.Version != ExportVersion {
		return domain.Config{}, fmt.Errorf("versão de exportação não suportada: %d", bundle.Version)
	}
	return SanitizeForExport(bundle.Config)
}
