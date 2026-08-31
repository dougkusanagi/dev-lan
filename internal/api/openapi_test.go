package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

type httpOperationSpec struct {
	method         string
	path           string
	operationID    string
	statuses       []int
	requestSchema  string
	responseMedia  string
	responseSchema string
}

var canonicalHTTPOperations = []httpOperationSpec{
	{http.MethodGet, "/api/v1/health", "health", []int{200, 403, 405}, "", "application/json", "HealthResponse"},
	{http.MethodGet, "/api/v1/status", "getStatus", []int{200, 403, 405, 500}, "", "application/json", "SystemStatus"},
	{http.MethodGet, "/api/v1/topology", "getTopology", []int{200, 403, 405}, "", "application/json", "TopologySnapshot"},
	{http.MethodGet, "/api/v1/overview", "getOverview", []int{200, 403, 405, 500}, "", "application/json", "Overview"},
	{http.MethodGet, "/api/v1/operations/{operationId}", "getOperation", []int{200, 400, 403, 404, 405}, "", "application/json", "MutationResult"},
	{http.MethodGet, "/api/v1/events", "events", []int{200, 403, 405, 500}, "", "text/event-stream", "string"},
	{http.MethodGet, "/api/v1/projects", "getProjects", []int{200, 403, 405, 500}, "", "application/json", "[]ProjectInfo"},
	{http.MethodGet, "/api/v1/projects/logs", "getProjectLogs", []int{200, 403, 405}, "", "application/json", "LogsResponse"},
	{http.MethodPost, "/api/v1/projects/link", "linkProject", []int{200, 400, 401, 403, 405, 409}, "LinkProjectRequest", "application/json", "MessageResponse"},
	{http.MethodPost, "/api/v1/projects/unlink", "unlinkProject", []int{200, 400, 401, 403, 405, 409}, "ProjectNameRequest", "application/json", "MessageResponse"},
	{http.MethodPost, "/api/v1/projects/hide", "hideProject", []int{200, 400, 401, 403, 405, 409}, "ProjectNameRequest", "application/json", "MessageResponse"},
	{http.MethodPost, "/api/v1/projects/unhide", "unhideProject", []int{200, 400, 401, 403, 405, 409}, "ProjectPathRequest", "application/json", "MessageResponse"},
	{http.MethodPost, "/api/v1/projects/config", "saveProjectConfig", []int{200, 202, 400, 401, 403, 405, 409}, "ProjectConfigUpdate", "application/json", "ProjectConfigResponse"},
	{http.MethodPost, "/api/v1/projects/start", "startDev", []int{202, 400, 401, 403, 405}, "ProjectOperationRequest", "application/json", "MutationResult"},
	{http.MethodPost, "/api/v1/projects/stop", "stopDev", []int{202, 400, 401, 403, 405}, "ProjectOperationRequest", "application/json", "MutationResult"},
	{http.MethodPost, "/api/v1/projects/restart", "restartDev", []int{202, 400, 401, 403, 405}, "ProjectOperationRequest", "application/json", "MutationResult"},
	{http.MethodPost, "/api/v1/projects/build", "buildProject", []int{200, 400, 401, 403, 405, 409}, "ProjectNameRequest", "application/json", "OutputResponse"},
	{http.MethodPost, "/api/v1/projects/deps", "installDeps", []int{200, 400, 401, 403, 405, 409}, "ProjectNameRequest", "application/json", "OutputResponse"},
	{http.MethodPost, "/api/v1/projects/tls", "setProjectTLS", []int{202, 400, 401, 403, 405}, "ProjectTLSRequest", "application/json", "MutationResult"},
	{http.MethodPost, "/api/v1/parks/park", "parkDir", []int{200, 400, 401, 403, 405, 409}, "ProjectPathRequest", "application/json", "MessageResponse"},
	{http.MethodPost, "/api/v1/parks/unpark", "unparkDir", []int{200, 400, 401, 403, 405, 409}, "ProjectPathRequest", "application/json", "MessageResponse"},
	{http.MethodGet, "/api/v1/config", "getGlobalConfig", []int{200, 403, 405, 500}, "", "application/json", "GlobalConfig"},
	{http.MethodPost, "/api/v1/config", "saveGlobalConfig", []int{200, 400, 401, 403, 405, 409}, "GlobalConfig", "application/json", "MessageOnlyResponse"},
	{http.MethodPost, "/api/v1/config/export", "exportConfigJSON", []int{200, 401, 403, 405, 500}, "", "application/json", "ExportBundle"},
	{http.MethodPost, "/api/v1/config/import", "importConfigJSON", []int{200, 400, 401, 403, 405, 409}, "ExportBundle", "application/json", "MessageResponse"},
	{http.MethodPost, "/api/v1/reload", "reload", []int{200, 401, 403, 405, 409}, "", "application/json", "ReloadResponse"},
	{http.MethodGet, "/api/v1/php/versions", "getPHPVersions", []int{200, 403, 405, 500}, "", "application/json", "[]PHPVersion"},
	{http.MethodPost, "/api/v1/php/install", "installPHP", []int{200, 400, 401, 403, 405, 409}, "PHPInstallRequest", "application/json", "MessageResponse"},
	{http.MethodPost, "/api/v1/php/remove", "removePHP", []int{200, 400, 401, 403, 405, 409}, "PHPVersionRequest", "application/json", "MessageResponse"},
	{http.MethodPost, "/api/v1/php/default", "setPHPDefault", []int{200, 400, 401, 403, 405, 409}, "PHPVersionRequest", "application/json", "MessageResponse"},
	{http.MethodGet, "/api/v1/metrics", "getMetrics", []int{200, 400, 403, 405}, "", "application/json", "MetricsSnapshot"},
	{http.MethodGet, "/api/v1/doctor", "runDoctor", []int{200, 403, 405, 500}, "", "application/json", "[]DoctorCheck"},
	{http.MethodPost, "/api/v1/doctor/fix", "applyDoctorFix", []int{200, 400, 401, 403, 405, 409}, "DoctorFixRequest", "application/json", "MessageOnlyResponse"},
	{http.MethodGet, "/api/v1/security/audit", "getSecurityAudit", []int{200, 403, 405, 500}, "", "application/json", "LogsResponse"},
	{http.MethodPost, "/api/v1/security/trust", "trustCA", []int{200, 401, 403, 405, 409}, "", "application/json", "MessageOnlyResponse"},
	{http.MethodPost, "/api/v1/command", "command", []int{200, 400, 401, 403, 404, 405, 409, 500}, "CommandRequest", "application/json", "CommandResponse"},
}

var legacyHTTPAliases = map[string]string{
	"/v1/health":                   "/api/v1/health",
	"/v1/status":                   "/api/v1/status",
	"/v1/topology":                 "/api/v1/topology",
	"/v1/overview":                 "/api/v1/overview",
	"/v1/operations/{operationId}": "/api/v1/operations/{operationId}",
	"/v1/events":                   "/api/v1/events",
	"/v1/projects":                 "/api/v1/projects",
	"/v1/config":                   "/api/v1/config",
	"/v1/reload":                   "/api/v1/reload",
	"/v1/command":                  "/api/v1/command",
}

func TestOpenAPIIsValidAndMatchesHTTPHandlers(t *testing.T) {
	document := loadOpenAPI(t)
	if got := len(canonicalHTTPOperations); got != 36 {
		t.Fatalf("inventário canônico tem %d operações, quer 36", got)
	}

	seen := make(map[string]bool, len(canonicalHTTPOperations))
	for _, spec := range canonicalHTTPOperations {
		key := spec.method + " " + spec.path
		if seen[key] {
			t.Fatalf("operação duplicada no inventário: %s", key)
		}
		seen[key] = true

		pathItem := document.Paths.Find(spec.path)
		if pathItem == nil {
			t.Errorf("rota canônica ausente no OpenAPI: %s", spec.path)
			continue
		}
		operation := pathItem.GetOperation(spec.method)
		if operation == nil {
			t.Errorf("método ausente no OpenAPI: %s", key)
			continue
		}
		if operation.OperationID != spec.operationID {
			t.Errorf("%s operationId=%q, quer %q", key, operation.OperationID, spec.operationID)
		}
		if spec.method == http.MethodGet {
			if operation.Security == nil || len(*operation.Security) != 0 {
				t.Errorf("%s deve declarar security: []", key)
			}
		} else if operation.Security != nil {
			t.Errorf("%s deve herdar autenticação Bearer ou CSRF global", key)
		}
		assertOperationStatuses(t, key, operation, spec.statuses)
		assertRequestSchema(t, key, operation, spec.requestSchema)
		assertResponseSchema(t, key, operation, spec)
	}

	for alias, canonical := range legacyHTTPAliases {
		pathItem := document.Paths.Value(alias)
		if pathItem == nil {
			t.Errorf("alias legado ausente no OpenAPI: %s", alias)
			continue
		}
		if got := pathItem.Extensions["x-devlan-alias-for"]; got != canonical {
			t.Errorf("alias %s aponta para %v, quer %q", alias, got, canonical)
		}
	}
}

func TestTransitionalFrontendManifestIsCoveredByOpenAPI(t *testing.T) {
	var manifest struct {
		Operations map[string]struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		} `json:"operations"`
	}
	data, err := os.ReadFile("contract.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	document := loadOpenAPI(t)
	for name, bridge := range manifest.Operations {
		if bridge.Method == "LOCAL" {
			continue
		}
		pathItem := document.Paths.Find(bridge.Path)
		if pathItem == nil || pathItem.GetOperation(bridge.Method) == nil {
			t.Errorf("ponte TypeScript %s não está coberta pelo OpenAPI: %s %s", name, bridge.Method, bridge.Path)
		}
	}
}

func TestHTTPRouteRegistrationMatchesInventory(t *testing.T) {
	data, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`HandleFunc\("([^"]+)"`).FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal("nenhum HandleFunc encontrado em routes.go")
	}

	wantCanonical := make(map[string]bool)
	for _, spec := range canonicalHTTPOperations {
		wantCanonical[spec.path] = true
	}
	wantAliases := make(map[string]bool)
	for alias := range legacyHTTPAliases {
		wantAliases[alias] = true
	}
	gotCanonical := make(map[string]bool)
	gotAliases := make(map[string]bool)
	for _, match := range matches {
		route := match[1]
		switch {
		case strings.HasPrefix(route, "/api/v1/"):
			if strings.HasSuffix(route, "/operations/") {
				route = strings.TrimSuffix(route, "/") + "/{operationId}"
			}
			gotCanonical[route] = true
		case strings.HasPrefix(route, "/v1/"):
			if strings.HasSuffix(route, "/operations/") {
				route = strings.TrimSuffix(route, "/") + "/{operationId}"
			}
			gotAliases[route] = true
		}
	}
	if !slices.Equal(sortedBoolKeys(gotCanonical), sortedBoolKeys(wantCanonical)) {
		t.Errorf("rotas canônicas registradas=%v, inventário=%v", sortedBoolKeys(gotCanonical), sortedBoolKeys(wantCanonical))
	}
	if !slices.Equal(sortedBoolKeys(gotAliases), sortedBoolKeys(wantAliases)) {
		t.Errorf("aliases registrados=%v, inventário=%v", sortedBoolKeys(gotAliases), sortedBoolKeys(wantAliases))
	}
}

func TestOpenAPIUsesHandlerQueryParameterNames(t *testing.T) {
	document := loadOpenAPI(t)
	want := map[string][]string{
		"GET /api/v1/overview":       {"filter"},
		"GET /api/v1/projects":       {"filter"},
		"GET /api/v1/projects/logs":  {"name", "lines"},
		"GET /api/v1/metrics":        {"project", "range"},
		"GET /api/v1/doctor":         {"project"},
		"GET /api/v1/security/audit": {"lines"},
	}
	for key, names := range want {
		parts := strings.SplitN(key, " ", 2)
		operation := document.Paths.Find(parts[1]).GetOperation(parts[0])
		got := make([]string, 0, len(operation.Parameters))
		for _, parameter := range operation.Parameters {
			if parameter.Value != nil && parameter.Value.In == "query" {
				got = append(got, parameter.Value.Name)
			}
		}
		slices.Sort(got)
		wantNames := slices.Clone(names)
		slices.Sort(wantNames)
		if !slices.Equal(got, wantNames) {
			t.Errorf("%s query parameters=%v, quer %v", key, got, wantNames)
		}
	}
}

func sortedBoolKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func loadOpenAPI(t *testing.T) *openapi3.T {
	t.Helper()
	path := filepath.Join("..", "..", "api", "openapi.yaml")
	document, err := openapi3.NewLoader().LoadFromFile(path)
	if err != nil {
		t.Fatalf("carregar OpenAPI: %v", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		t.Fatalf("validar OpenAPI: %v", err)
	}
	return document
}

func assertOperationStatuses(t *testing.T, key string, operation *openapi3.Operation, want []int) {
	t.Helper()
	got := make([]int, 0, operation.Responses.Len())
	for _, status := range operation.Responses.Keys() {
		var parsed int
		if _, err := fmt.Sscanf(status, "%d", &parsed); err != nil {
			t.Errorf("%s tem status não numérico %q", key, status)
			continue
		}
		got = append(got, parsed)
	}
	slices.Sort(got)
	want = slices.Clone(want)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s statuses=%v, quer %v", key, got, want)
	}
}

func assertRequestSchema(t *testing.T, key string, operation *openapi3.Operation, want string) {
	t.Helper()
	if want == "" {
		if operation.RequestBody != nil {
			t.Errorf("%s não deve declarar requestBody", key)
		}
		return
	}
	if operation.RequestBody == nil || operation.RequestBody.Value == nil {
		t.Errorf("%s não declara requestBody", key)
		return
	}
	media := operation.RequestBody.Value.Content.Get("application/json")
	if media == nil || schemaName(media.Schema) != want {
		t.Errorf("%s request schema=%q, quer %q", key, mediaSchemaName(media), want)
	}
}

func assertResponseSchema(t *testing.T, key string, operation *openapi3.Operation, spec httpOperationSpec) {
	t.Helper()
	status := spec.statuses[0]
	response := operation.Responses.Status(status)
	if response == nil || response.Value == nil {
		t.Errorf("%s não declara resposta %d", key, status)
		return
	}
	media := response.Value.Content.Get(spec.responseMedia)
	if media == nil {
		t.Errorf("%s resposta %d não declara %s", key, status, spec.responseMedia)
		return
	}
	if got := schemaName(media.Schema); got != spec.responseSchema {
		t.Errorf("%s response schema=%q, quer %q", key, got, spec.responseSchema)
	}
}

func mediaSchemaName(media *openapi3.MediaType) string {
	if media == nil {
		return ""
	}
	return schemaName(media.Schema)
}

func schemaName(schema *openapi3.SchemaRef) string {
	if schema == nil {
		return ""
	}
	if schema.Ref != "" {
		return strings.TrimPrefix(schema.Ref, "#/components/schemas/")
	}
	if schema.Value == nil {
		return ""
	}
	if schema.Value.Type != nil && schema.Value.Type.Is("array") {
		return "[]" + schemaName(schema.Value.Items)
	}
	if schema.Value.Type != nil && schema.Value.Type.Is("string") {
		return "string"
	}
	return "inline"
}
