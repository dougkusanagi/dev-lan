package api

import (
	_ "embed"
)

// contractManifest is the single checked-in description used to generate the
// browser types and to validate the JSON tags of the Go views in tests.
//
//go:embed contract.json
var contractManifest []byte

// ContractManifest returns a copy so callers cannot mutate the embedded
// contract used by diagnostics or tooling.
func ContractManifest() []byte {
	return append([]byte(nil), contractManifest...)
}
