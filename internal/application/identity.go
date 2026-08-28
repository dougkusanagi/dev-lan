package application

import (
	"crypto/rand"
	"encoding/hex"
	"runtime"
	"time"
)

// NewOperationID gives transports a stable, opaque id for idempotent command
// retries without exposing the operation registry implementation.
func NewOperationID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
}

// RuntimeDescription is diagnostic metadata, not an operating-system adapter.
func RuntimeDescription() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
