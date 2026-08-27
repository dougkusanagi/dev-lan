package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/detect"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

func TestOverviewRouteUsesOneReadModelAndBatchedWSLCalls(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	service := app.New(t.TempDir())

	flags := make([]string, 17)
	for index := range flags {
		flags[index] = "0"
	}
	flags[14] = "1" // dist/index.html: a static discovered project.
	discoveryLine := strings.Join(append([]string{"/home/dev/static"}, flags...), "\t") + "\n"
	invoker := &overviewTestInvoker{function: func(_ context.Context, args []string) (string, error) {
		joined := strings.Join(args, "\x00")
		if strings.Contains(joined, "for d in") {
			// Keep the fake response deterministic while retaining the real
			// WSL parser and detector path under test.
			return discoveryLine, nil
		}
		return strings.Repeat("0\n", 15), nil // PHP/FPM availability batch.
	}}
	service.WSL.Invoker = invoker
	service.Detector = detect.Detector{Inspector: detect.SmartInspector{WSL: service.WSL}}
	service.PHP = platform.NewWSLPHPManager(service.WSL)
	service.Dev = platform.NewWSLDevManager(service.WSL)

	cfg := domain.NewConfig()
	cfg.Parks = []domain.Park{{Path: "/home/dev"}}
	if err := service.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/overview", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	New(service).Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("overview rejeitado: %d %s", recorder.Code, recorder.Body.String())
	}
	var view OverviewView
	if err := json.Unmarshal(recorder.Body.Bytes(), &view); err != nil {
		t.Fatalf("overview não é JSON válido: %v", err)
	}
	if len(view.Projects) != 1 || view.Projects[0].Name != "static" {
		t.Fatalf("projeto descoberto inesperado: %#v", view.Projects)
	}

	stats := service.WSL.StatsSnapshot()
	if stats.TotalCalls != 2 || stats.Operations[platform.WSLOperationDiscovery].Calls != 1 || stats.Operations[platform.WSLOperationStatus].Calls != 1 {
		t.Fatalf("poll agregado abriu spawns inesperados: %#v", stats)
	}

	// A second poll inside the hot/cold TTLs is served from the materialized
	// snapshots and must not cross the WSL boundary again.
	second := httptest.NewRecorder()
	New(service).Handler().ServeHTTP(second, request.Clone(request.Context()))
	if second.Code != http.StatusOK {
		t.Fatalf("segundo overview rejeitado: %d", second.Code)
	}
	if secondStats := service.WSL.StatsSnapshot(); secondStats.TotalCalls != stats.TotalCalls {
		t.Fatalf("snapshot quente abriu spawns adicionais: antes=%#v depois=%#v", stats, secondStats)
	}
	if !strings.Contains(second.Body.String(), `"cache":"hot+cold"`) {
		t.Fatalf("overview não informou cache quente/frio: %s", second.Body.String())
	}
}

type overviewTestInvoker struct {
	function func(context.Context, []string) (string, error)
	calls    int
}

func (i *overviewTestInvoker) Run(ctx context.Context, args ...string) (string, error) {
	i.calls++
	return i.function(ctx, args)
}
