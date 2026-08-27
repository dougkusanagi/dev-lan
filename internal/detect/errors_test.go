package detect

import (
	"context"
	"errors"
	"os"
	"testing"
)

type failingInspector struct{ err error }

func (f failingInspector) Exists(context.Context, string, string) (bool, error) { return false, f.err }
func (f failingInspector) Directory(context.Context, string) (bool, error)      { return false, f.err }
func (f failingInspector) ListDirectories(context.Context, string) ([]string, error) {
	return nil, f.err
}
func (f failingInspector) ReadFile(context.Context, string, string) ([]byte, error) {
	return nil, f.err
}

func TestDiscoveryErrorsHaveStableDiagnosticKinds(t *testing.T) {
	tests := []struct {
		err  error
		kind FailureKind
	}{
		{context.DeadlineExceeded, FailureTimeout}, {context.Canceled, FailureCanceled},
		{os.ErrPermission, FailurePermission}, {os.ErrNotExist, FailureNotFound}, {errors.New("boom"), FailureInternal},
	}
	for _, test := range tests {
		_, err := (Detector{Inspector: failingInspector{test.err}}).BatchDiscoverProjects(context.Background(), "/park")
		if !IsFailure(err, test.kind) {
			t.Errorf("%v classificado como %v: %v", test.err, test.kind, err)
		}
		if !errors.Is(err, test.err) {
			t.Errorf("causa não preservada: %v", err)
		}
	}
	_, err := (Detector{Inspector: StaticInspector{}}).DetectProject(context.Background(), "/empty")
	if !IsFailure(err, FailureInvalid) {
		t.Fatalf("projeto inválido sem categoria: %v", err)
	}
}
