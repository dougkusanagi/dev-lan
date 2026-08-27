package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

func TestProjectConfigRoutePortOverrideAndAutoResetUseOneContract(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	dataDir := t.TempDir()
	projectPath := filepath.Join(dataDir, "catalogo")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	mode := domain.ModeStatic
	service := app.New(dataDir)
	cfg := domain.NewConfig()
	cfg.LANAddress = "192.168.1.50"
	cfg.Projects = []domain.Project{{Name: "catalogo", Path: filepath.ToSlash(projectPath), Mode: &mode}}
	if err := service.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}

	server := New(service)
	setRoute := func(payload map[string]any) *httptest.ResponseRecorder {
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/projects/config", bytes.NewReader(body))
		request.RemoteAddr = "127.0.0.1:1234"
		csrf := "route-contract-csrf"
		request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf})
		request.Header.Set(csrfHeaderName, csrf)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		return recorder
	}

	if response := setRoute(map[string]any{"name": "catalogo", "routePort": 8123}); response.Code != http.StatusOK {
		t.Fatalf("override de porta rejeitado: %d %s", response.Code, response.Body.String())
	}
	views, err := BuildProjectViews(context.Background(), service, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Port != 8123 || views[0].RoutePortOverride != 8123 {
		t.Fatalf("view não expôs override: %#v", views)
	}

	if response := setRoute(map[string]any{"name": "catalogo", "routePortAuto": true}); response.Code != http.StatusOK {
		t.Fatalf("retorno à porta automática rejeitado: %d %s", response.Code, response.Body.String())
	}
	views, err = BuildProjectViews(context.Background(), service, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Port != 8080 || views[0].RoutePortOverride != 0 {
		t.Fatalf("view não restaurou alocação automática: %#v", views)
	}
}

func TestContractErrorKeepsRollbackStatusInHTTPResponse(t *testing.T) {
	result := app.ApplyResult{Status: "rolled_back", Revision: 7, Warnings: []string{"configuração anterior restaurada"}}
	recorder := httptest.NewRecorder()
	writeApplyError(recorder, http.StatusConflict, result, context.Canceled)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"status":"rolled_back"`) {
		t.Fatalf("resposta de rollback não é diagnosticável: %d %s", recorder.Code, recorder.Body.String())
	}
}
