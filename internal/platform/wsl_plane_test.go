package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type wslTestInvoker struct {
	mu        sync.Mutex
	calls     int
	responses []string
	function  func(context.Context, []string) (string, error)
}

func (i *wslTestInvoker) Run(ctx context.Context, args ...string) (string, error) {
	i.mu.Lock()
	i.calls++
	responseIndex := i.calls - 1
	responses := append([]string(nil), i.responses...)
	function := i.function
	i.mu.Unlock()
	if function != nil {
		return function(ctx, args)
	}
	if responseIndex < len(responses) {
		return responses[responseIndex], nil
	}
	return "", nil
}

func (i *wslTestInvoker) Calls() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.calls
}

func (i *wslTestInvoker) Reset() {
	i.mu.Lock()
	i.calls = 0
	i.mu.Unlock()
}

func TestWSLBatchChecksUseOneSpawn(t *testing.T) {
	invoker := &wslTestInvoker{responses: []string{"1\n0\n1\n"}}
	runner := NewWSLRunner("wsl.exe", "Ubuntu-24.04")
	runner.Invoker = invoker

	found, err := runner.HasCommands(context.Background(), "node", "npm", "bun")
	if err != nil {
		t.Fatal(err)
	}
	if !found["node"] || found["npm"] || !found["bun"] {
		t.Fatalf("resultado de disponibilidade inesperado: %#v", found)
	}
	if got := invoker.Calls(); got != 1 {
		t.Fatalf("HasCommands deveria iniciar um único wsl.exe, iniciou %d", got)
	}
	stats := runner.StatsSnapshot()
	if stats.TotalCalls != 1 || stats.Operations[WSLOperationStatus].Calls != 1 {
		t.Fatalf("inventário WSL inesperado: %#v", stats)
	}
}

func TestWSLDevStatusesAreGroupedAndParsed(t *testing.T) {
	invoker := &wslTestInvoker{responses: []string{
		"vite\t123\tstarting\napi\t0\tstopped\n",
	}}
	runner := NewWSLRunner("wsl.exe", "")
	runner.Invoker = invoker
	items, err := runner.DevStatuses(context.Background(),
		WSLDevStatusRequest{Name: "vite", PIDFile: "/tmp/devlan-vite.pid"},
		WSLDevStatusRequest{Name: "api", PIDFile: "/tmp/devlan-api.pid"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].PID != 123 || items[0].State != StateStarting || items[1].State != StateStopped {
		t.Fatalf("status WSL inesperado: %#v", items)
	}
	if invoker.Calls() != 1 {
		t.Fatalf("status agrupado iniciou %d processos WSL", invoker.Calls())
	}
}

func TestWSLExecutionContractIsVersionedIdempotentAndCancelable(t *testing.T) {
	invoker := &wslTestInvoker{responses: []string{"ok\n"}}
	runner := NewWSLRunner("wsl.exe", "")
	runner.Invoker = invoker

	request := WSLExecutionRequest{
		Version:    WSLPlaneProtocolVersion,
		RequestID:  "reload-1",
		Operation:  WSLOperationReload,
		Command:    []string{"/bin/true"},
		Idempotent: true,
	}
	first, err := runner.Execute(context.Background(), request)
	if err != nil || first.Status != "ok" || first.Output != "ok\n" {
		t.Fatalf("primeira execução inesperada: %#v, %v", first, err)
	}
	second, err := runner.Execute(context.Background(), request)
	if err != nil || second.Status != "ok" || invoker.Calls() != 1 {
		t.Fatalf("retry idempotente iniciou novo processo: %#v, %v, calls=%d", second, err, invoker.Calls())
	}

	request.Command = []string{"/bin/false"}
	if _, err := runner.Execute(context.Background(), request); !errors.Is(err, ErrExecutionConflict) {
		t.Fatalf("request id reutilizado com payload diferente deveria falhar com conflito: %v", err)
	}

	badVersion := request
	badVersion.RequestID = "version-1"
	badVersion.Version = WSLPlaneProtocolVersion + 1
	if _, err := runner.Execute(context.Background(), badVersion); !errors.Is(err, ErrExecutionProtocol) {
		t.Fatalf("versão incompatível não foi rejeitada: %v", err)
	}

	cancelInvoker := &wslTestInvoker{function: func(ctx context.Context, _ []string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}
	cancelRunner := NewWSLRunner("wsl.exe", "")
	cancelRunner.Invoker = cancelInvoker
	cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	canceledRequest := WSLExecutionRequest{
		Version:   WSLPlaneProtocolVersion,
		RequestID: "timeout-1",
		Operation: WSLOperationStatus,
		Command:   []string{"/bin/sleep", "60"},
	}
	response, err := cancelRunner.Execute(cancelCtx, canceledRequest)
	if !errors.Is(err, context.DeadlineExceeded) || response.Status != "canceled" || response.ErrorKind != string(WSLFailureTimeout) {
		t.Fatalf("cancelamento não foi estruturado: %#v, %v", response, err)
	}
}

func TestWSLStoppedAndRestartedAreDiagnosable(t *testing.T) {
	var calls int
	invoker := &wslTestInvoker{function: func(_ context.Context, _ []string) (string, error) {
		calls++
		if calls == 1 {
			return "", fmt.Errorf("%w: distribuição parada", ErrUnavailable)
		}
		return "reconnected", nil
	}}
	runner := NewWSLRunner("wsl.exe", "Ubuntu-24.04")
	runner.Invoker = invoker
	request := WSLExecutionRequest{
		Version:    WSLPlaneProtocolVersion,
		RequestID:  "restart-1",
		Operation:  WSLOperationStatus,
		Command:    []string{"/bin/true"},
		Idempotent: true,
	}
	if _, err := runner.Execute(context.Background(), request); err == nil {
		t.Fatal("WSL parado deveria falhar")
	} else {
		var executionErr *WSLExecutionError
		if !errors.As(err, &executionErr) || executionErr.Kind != WSLFailureUnavailable || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("erro de WSL parado não foi diagnosticável: %v", err)
		}
	}
	response, err := runner.Execute(context.Background(), request)
	if err != nil || response.Output != "reconnected" || invoker.Calls() != 2 {
		t.Fatalf("WSL não recuperou após reinício: %#v, %v", response, err)
	}
}

func TestWSLMissingDistributionIsUnavailable(t *testing.T) {
	runner := NewWSLRunner("wsl.exe", "missing")
	runner.Invoker = &wslTestInvoker{function: func(_ context.Context, _ []string) (string, error) {
		return "", fmt.Errorf("wsl.exe: there is no distribution with the supplied name")
	}}
	_, err := runner.Run(context.Background(), "/bin/true")
	if err == nil {
		t.Fatal("distribuição ausente deveria falhar")
	}
	var executionErr *WSLExecutionError
	if !errors.As(err, &executionErr) || executionErr.Kind != WSLFailureUnavailable || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("distribuição ausente não foi classificada como indisponível: %v", err)
	}
}

func TestWSLPHPListUsesOneAvailabilitySpawn(t *testing.T) {
	invoker := &wslTestInvoker{responses: []string{strings.Repeat("0\n", 15)}}
	runner := NewWSLRunner("wsl.exe", "")
	runner.Invoker = invoker
	items, err := (WSLPHPManager{WSL: runner}).List(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatalf("lista PHP inesperada: %#v, %v", items, err)
	}
	if invoker.Calls() != 1 {
		t.Fatalf("lista PHP deveria usar um spawn agrupado, usou %d", invoker.Calls())
	}
}

func BenchmarkWSLBatching(b *testing.B) {
	const items = 16
	invoker := &wslTestInvoker{function: func(_ context.Context, _ []string) (string, error) {
		return strings.Repeat("1\n", items), nil
	}}
	runner := NewWSLRunner("wsl.exe", "")
	runner.Invoker = invoker

	b.Run("direct", func(b *testing.B) {
		invoker.Reset()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			for item := 0; item < items; item++ {
				if _, err := runner.HasCommand(context.Background(), fmt.Sprintf("tool-%d", item)); err != nil {
					b.Fatal(err)
				}
			}
		}
		b.StopTimer()
		b.ReportMetric(float64(invoker.Calls())/float64(b.N), "spawns/op")
	})
	b.Run("batch", func(b *testing.B) {
		commands := make([]string, items)
		for index := range commands {
			commands[index] = fmt.Sprintf("tool-%d", index)
		}
		invoker.Reset()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			if _, err := runner.HasCommands(context.Background(), commands...); err != nil {
				b.Fatal(err)
			}
		}
		b.StopTimer()
		b.ReportMetric(float64(invoker.Calls())/float64(b.N), "spawns/op")
	})
}
