package app

import (
	"context"
	"errors"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/config"
	"github.com/dougkusanagi/dev-lan/internal/detect"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

type countingReconcileRunner struct {
	calls int
}

func (r *countingReconcileRunner) Run(context.Context, ...string) (string, error) {
	r.calls++
	return "", nil
}

func TestSaveConfigAndApplyRejectsStaleRevisionBeforeStage(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{}}
	service.Caddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	ctx := context.Background()

	initial := domain.NewConfig()
	if _, err := service.SaveConfigAndApply(ctx, initial, false); err != nil {
		t.Fatalf("persistência inicial: %v", err)
	}
	stale, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}

	current := stale
	current.LANAddress = "192.0.2.10"
	if _, err := service.SaveConfigAndApply(ctx, current, false); err != nil {
		t.Fatalf("persistência concorrente: %v", err)
	}

	runner := &countingReconcileRunner{}
	service.Caddy = platform.CaddyClient{Runner: runner, WSL: true}
	stale.LANAddress = "192.0.2.11"
	if _, err := service.SaveConfigAndApply(ctx, stale, false); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatalf("esperava conflito de revisão antes do apply, obtive %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("plano obsoleto chamou o adaptador Caddy %d vez(es)", runner.calls)
	}

	loaded, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LANAddress != current.LANAddress || loaded.Revision != current.Revision+1 {
		t.Fatalf("conflito alterou o estado autoritativo: %#v", loaded)
	}
}

func TestSaveConfigAndApplyRollsBackCommittedRevisionOnVerifyFailure(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{}}
	service.Caddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	ctx := context.Background()

	if _, err := service.SaveConfigAndApply(ctx, domain.NewConfig(), false); err != nil {
		t.Fatalf("persistência inicial: %v", err)
	}
	previous, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	previousCaddy, err := service.Store.GeneratedCaddy()
	if err != nil {
		t.Fatal(err)
	}

	candidate := previous
	candidate.LANAddress = "192.0.2.10"
	service.Caddy = platform.CaddyClient{
		Runner: &characterizationToggleRunner{failAfter: 3},
		WSL:    true,
	}
	result, err := service.SaveConfigAndApply(ctx, candidate, true)
	if err == nil || result.Status != "rolled_back" {
		t.Fatalf("falha de verify não acionou rollback: result=%#v err=%v", result, err)
	}

	recovered, err := service.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Revision != previous.Revision || recovered.LANAddress != previous.LANAddress {
		t.Fatalf("revisão anterior não foi restaurada: %#v; anterior=%#v", recovered, previous)
	}
	recoveredCaddy, err := service.Store.GeneratedCaddy()
	if err != nil {
		t.Fatal(err)
	}
	if recoveredCaddy != previousCaddy {
		t.Fatal("Caddy gerado anterior não foi restaurado após falha de verify")
	}
}
