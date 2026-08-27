package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
)

func TestAsyncOperationAcceptsRetriesWithoutDuplicatingWork(t *testing.T) {
	service := app.New(t.TempDir())
	server := New(service)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	work := func(ctx context.Context) (uint64, []string, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return 17, []string{"aplicado"}, nil
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		}
	}

	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/projects/tls", nil)
	first := httptest.NewRecorder()
	server.startAsyncOperation(first, request, "tls", "catalogo", "retry-id", time.Second, work)
	if first.Code != http.StatusAccepted {
		t.Fatalf("aceite inesperado: %d %s", first.Code, first.Body.String())
	}
	var accepted MutationResult
	if err := json.Unmarshal(first.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.OperationID != "retry-id" || accepted.Phase != "accepted" {
		t.Fatalf("envelope inicial inesperado: %#v", accepted)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("operação não iniciou")
	}

	second := httptest.NewRecorder()
	server.startAsyncOperation(second, request, "tls", "catalogo", "retry-id", time.Second, work)
	if second.Code != http.StatusAccepted {
		t.Fatalf("retry inesperado: %d %s", second.Code, second.Body.String())
	}
	var retry MutationResult
	if err := json.Unmarshal(second.Body.Bytes(), &retry); err != nil {
		t.Fatal(err)
	}
	if retry.OperationID != accepted.OperationID {
		t.Fatalf("retry mudou operationId: %#v", retry)
	}

	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state, ok := service.Operation("retry-id")
		if ok && state.Phase == "ready" {
			if state.Revision != 17 || len(state.Warnings) != 1 {
				t.Fatalf("estado final incompleto: %#v", state)
			}
			if calls.Load() != 1 {
				t.Fatalf("retry executou o trabalho %d vezes", calls.Load())
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("operação não chegou ao estado final")
}

func TestAsyncOperationTimeoutAfterCommitIsVisibleAsRollback(t *testing.T) {
	service := app.New(t.TempDir())
	server := New(service)
	if _, _, err := service.BeginOperation("timeout-id", "start", "catalogo"); err != nil {
		t.Fatal(err)
	}
	server.runAsyncOperation("timeout-id", "start", "catalogo", 15*time.Millisecond,
		func(ctx context.Context) (uint64, []string, error) {
			<-ctx.Done()
			return 23, []string{"commit confirmado antes do timeout"}, ctx.Err()
		})

	state, ok := service.Operation("timeout-id")
	if !ok {
		t.Fatal("operação não foi retida")
	}
	if state.Phase != "rolled_back" || state.Revision != 23 || state.Error == "" {
		t.Fatalf("timeout não revelou estado autoritativo: %#v", state)
	}
	if len(state.Warnings) != 1 {
		t.Fatalf("warnings do timeout foram perdidos: %#v", state)
	}
}
