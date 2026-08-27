package detect

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/dougkusanagi/dev-lan/internal/platform"
)

type FailureKind string

const (
	FailureInvalid     FailureKind = "invalid"
	FailureNotFound    FailureKind = "not_found"
	FailurePermission  FailureKind = "permission"
	FailureTimeout     FailureKind = "timeout"
	FailureCanceled    FailureKind = "canceled"
	FailureUnavailable FailureKind = "unavailable"
	FailureInternal    FailureKind = "internal"
)

// DiscoveryError gives the CLI/API a stable diagnostic category while
// preserving the underlying error for errors.Is and actionable detail.
type DiscoveryError struct {
	Kind      FailureKind
	Operation string
	Path      string
	Err       error
}

func (e *DiscoveryError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("discovery %s (%s) em %s: %v", e.Operation, e.Kind, e.Path, e.Err)
}
func (e *DiscoveryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func wrapDiscovery(operation, path string, err error) error {
	if err == nil {
		return nil
	}
	var existing *DiscoveryError
	if errors.As(err, &existing) {
		return err
	}
	kind := FailureInternal
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		kind = FailureTimeout
	case errors.Is(err, context.Canceled):
		kind = FailureCanceled
	case errors.Is(err, os.ErrPermission):
		kind = FailurePermission
	case errors.Is(err, os.ErrNotExist):
		kind = FailureNotFound
	case errors.Is(err, platform.ErrUnavailable):
		kind = FailureUnavailable
	}
	return &DiscoveryError{Kind: kind, Operation: operation, Path: path, Err: err}
}

func invalidDiscovery(operation, path string, err error) error {
	return &DiscoveryError{Kind: FailureInvalid, Operation: operation, Path: path, Err: err}
}

func IsFailure(err error, kind FailureKind) bool {
	var target *DiscoveryError
	return errors.As(err, &target) && target.Kind == kind
}
