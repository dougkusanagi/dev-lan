package app

import (
	"context"
	"testing"
	"time"
)

func TestOperationIDsAreIdempotentAndRetainTerminalState(t *testing.T) {
	service := New(t.TempDir())
	first, existed, err := service.BeginOperation("same-operation", "tls", "catalogo")
	if err != nil || existed {
		t.Fatalf("primeira operação inválida: %#v existed=%t err=%v", first, existed, err)
	}
	second, existed, err := service.BeginOperation("same-operation", "tls", "catalogo")
	if err != nil || !existed || second.OperationID != first.OperationID || second.Phase != "accepted" {
		t.Fatalf("repetição não foi idempotente: %#v existed=%t err=%v", second, existed, err)
	}

	terminal := service.UpdateOperation("same-operation", "ready", "ready", 9, map[string]string{"state": "authoritative"}, []string{"aviso"}, nil)
	if terminal.Revision != 9 || terminal.FinishedAt.IsZero() || terminal.ProjectState == nil {
		t.Fatalf("estado terminal incompleto: %#v", terminal)
	}
	retained, existed, err := service.BeginOperation("same-operation", "tls", "catalogo")
	if err != nil || !existed || retained.Phase != "ready" || retained.Revision != 9 {
		t.Fatalf("repetição sobrescreveu estado autoritativo: %#v existed=%t err=%v", retained, existed, err)
	}
	if _, _, err := service.BeginOperation("same-operation", "stop", "catalogo"); err == nil {
		t.Fatal("operationId foi aceito para outra operação")
	}
}

func TestOperationSubscriberDoesNotBlockTheMutation(t *testing.T) {
	service := New(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates, stop := service.SubscribeOperations(ctx)
	defer stop()
	if _, _, err := service.BeginOperation("event-operation", "start", "catalogo"); err != nil {
		t.Fatal(err)
	}
	select {
	case state := <-updates:
		if state.Phase != "accepted" {
			t.Fatalf("fase inicial inesperada: %#v", state)
		}
	case <-time.After(time.Second):
		t.Fatal("assinante não recebeu fase accepted")
	}
	service.UpdateOperation("event-operation", "starting", "starting", 0, nil, nil, nil)
	select {
	case state := <-updates:
		if state.Phase != "starting" {
			t.Fatalf("fase de progresso inesperada: %#v", state)
		}
	case <-time.After(time.Second):
		t.Fatal("assinante não recebeu progresso")
	}
}
