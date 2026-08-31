package api

import (
	_ "embed"
)

// contractManifest is the transitional bridge used to generate browser types
// until R-07d moves that generation to the canonical OpenAPI document. It is
// not the source of truth for the HTTP surface.
//
//go:embed contract.json
var contractManifest []byte

// ContractManifest returns a copy so callers cannot mutate the embedded
// contract used by diagnostics or tooling.
func ContractManifest() []byte {
	return append([]byte(nil), contractManifest...)
}
