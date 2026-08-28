// Package api exposes the loopback web server and authenticated API
// shared by the browser GUI, Windows service, CLI and WSL clients.
package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/dougkusanagi/dev-lan/frontend"
)

func (s *Server) Handler(token ...string) http.Handler {
	secret := ""
	if len(token) > 0 {
		secret = token[0]
	} else {
		loaded, err := readToken(s.service.APIEndpointFiles().Token)
		if err == nil {
			secret = loaded
		}
	}

	apiMux := http.NewServeMux()

	// Health & Status
	apiMux.HandleFunc("/api/v1/health", s.handleHealth)
	apiMux.HandleFunc("/v1/health", s.handleHealth)
	apiMux.HandleFunc("/api/v1/status", s.handleStatus)
	apiMux.HandleFunc("/v1/status", s.handleStatus)
	apiMux.HandleFunc("/api/v1/topology", s.handleTopology)
	apiMux.HandleFunc("/v1/topology", s.handleTopology)
	apiMux.HandleFunc("/api/v1/overview", s.handleOverview)
	apiMux.HandleFunc("/v1/overview", s.handleOverview)
	apiMux.HandleFunc("/api/v1/operations/", s.handleOperation)
	apiMux.HandleFunc("/v1/operations/", s.handleOperation)
	apiMux.HandleFunc("/api/v1/events", s.handleEvents)
	apiMux.HandleFunc("/v1/events", s.handleEvents)

	// Projects
	apiMux.HandleFunc("/api/v1/projects", s.handleProjects)
	apiMux.HandleFunc("/v1/projects", s.handleProjects)
	apiMux.HandleFunc("/api/v1/projects/logs", s.handleProjectLogs)
	apiMux.HandleFunc("/api/v1/projects/link", s.handleProjectLink)
	apiMux.HandleFunc("/api/v1/projects/unlink", s.handleProjectUnlink)
	apiMux.HandleFunc("/api/v1/projects/hide", s.handleProjectHide)
	apiMux.HandleFunc("/api/v1/projects/unhide", s.handleProjectUnhide)
	apiMux.HandleFunc("/api/v1/projects/config", s.handleProjectConfig)
	apiMux.HandleFunc("/api/v1/projects/start", s.handleProjectStart)
	apiMux.HandleFunc("/api/v1/projects/stop", s.handleProjectStop)
	apiMux.HandleFunc("/api/v1/projects/restart", s.handleProjectRestart)
	apiMux.HandleFunc("/api/v1/projects/build", s.handleProjectBuild)
	apiMux.HandleFunc("/api/v1/projects/deps", s.handleProjectDeps)
	apiMux.HandleFunc("/api/v1/projects/tls", s.handleProjectTLS)

	// Parks
	apiMux.HandleFunc("/api/v1/parks/park", s.handlePark)
	apiMux.HandleFunc("/api/v1/parks/unpark", s.handleUnpark)

	// Config
	apiMux.HandleFunc("/api/v1/config", s.handleConfig)
	apiMux.HandleFunc("/v1/config", s.handleConfig)
	apiMux.HandleFunc("/api/v1/config/export", s.handleConfigExport)
	apiMux.HandleFunc("/api/v1/config/import", s.handleConfigImport)
	apiMux.HandleFunc("/api/v1/reload", s.handleReload)
	apiMux.HandleFunc("/v1/reload", s.handleReload)

	// PHP
	apiMux.HandleFunc("/api/v1/php/versions", s.handlePHPVersions)
	apiMux.HandleFunc("/api/v1/php/install", s.handlePHPInstall)
	apiMux.HandleFunc("/api/v1/php/remove", s.handlePHPRemove)
	apiMux.HandleFunc("/api/v1/php/default", s.handlePHPDefault)

	// Metrics & Doctor
	apiMux.HandleFunc("/api/v1/metrics", s.handleMetrics)
	apiMux.HandleFunc("/api/v1/doctor", s.handleDoctor)
	apiMux.HandleFunc("/api/v1/doctor/fix", s.handleDoctorFix)

	// Security
	apiMux.HandleFunc("/api/v1/security/audit", s.handleSecurityAudit)
	apiMux.HandleFunc("/api/v1/security/trust", s.handleSecurityTrust)

	// WSL Command protocol
	apiMux.HandleFunc("/api/v1/command", s.handleCommand)
	apiMux.HandleFunc("/v1/command", s.handleCommand)

	// Global 404 for unhandled API routes
	apiMux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		writeJSONError(writer, http.StatusNotFound, "rota da API local não encontrada")
	})

	var distFS fs.FS
	if sub, err := fs.Sub(frontend.Assets, "dist"); err == nil {
		distFS = sub
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// 1. Loopback restriction (ui_access = local)
		if !isLoopback(request.RemoteAddr) {
			writeJSONError(writer, http.StatusForbidden, "API local aceita somente loopback")
			return
		}

		// 2. Host allowlist validation
		if !isValidHost(request.Host) {
			writeJSONError(writer, http.StatusForbidden, "Host não permitido: "+request.Host)
			return
		}

		// 3. Origin validation for browser requests
		if origin := request.Header.Get("Origin"); origin != "" {
			if !s.isValidOrigin(request.Context(), origin) {
				writeJSONError(writer, http.StatusForbidden, "Origem não permitida: "+origin)
				return
			}
		}

		// 4. Security headers on all responses
		setSecurityHeaders(writer)

		// 5. Route to API or SPA
		reqPath := path.Clean(request.URL.Path)
		if strings.HasPrefix(reqPath, "/api/") || strings.HasPrefix(reqPath, "/v1/") {
			// Limit body size for API
			if request.Body != nil {
				request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
			}
			// Authentication & CSRF check on mutations
			if isMutation(request.Method) {
				if !isAuthorizedOrCSRF(request, secret) {
					writeJSONError(writer, http.StatusUnauthorized, "token da API local ou CSRF ausente ou inválido")
					return
				}
			}
			apiMux.ServeHTTP(writer, request)
			return
		}

		// Serve embedded SPA static assets with history fallback
		s.serveSPA(writer, request, distFS)
	})
}
