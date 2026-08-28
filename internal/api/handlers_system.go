// Package api exposes the loopback web server and authenticated API
// shared by the browser GUI, Windows service, CLI and WSL clients.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func (s *Server) handleMetrics(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	project := request.URL.Query().Get("project")
	rawRange := request.URL.Query().Get("range")
	if rawRange == "" {
		rawRange = "1h"
	}
	snapshot, err := BuildMetricsSnapshot(s.service, project, rawRange)
	if err != nil {
		writeJSONError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, snapshot)
}

func (s *Server) handleDoctor(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	project := request.URL.Query().Get("project")
	checks, err := BuildDoctorChecksView(request.Context(), s.service, project)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, checks)
}

func (s *Server) handleDoctorFix(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Action string `json:"action"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	ctx := request.Context()
	switch input.Action {
	case "reload":
		_, err := s.service.Reload(ctx)
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	case "firewall":
		if err := s.service.ReconcileFirewall(ctx); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	case "topology", "topology-repair":
		if _, err := s.service.RepairM8(ctx); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	case "trust":
		if err := s.service.Trust(ctx); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	case "restart-dev":
		if input.Target != "" {
			if err := s.service.RestartDev(ctx, input.Target); err != nil {
				writeJSONError(writer, http.StatusConflict, err.Error())
				return
			}
		}
	default:
		_, _ = s.service.Reload(ctx)
	}
	s.InvalidateReadModelCache()
	writeJSON(writer, http.StatusOK, MessageOnlyResponse{Message: "Correção aplicada."})
}

func (s *Server) handleSecurityAudit(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	linesStr := request.URL.Query().Get("lines")
	lines := 100
	if l, err := strconv.Atoi(linesStr); err == nil && l > 0 {
		lines = l
	}
	logs, err := s.service.SecurityAuditLogs(request.Context(), lines)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, LogsResponse{Logs: logs})
}

func (s *Server) handleSecurityTrust(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	if err := s.service.Trust(request.Context()); err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	s.InvalidateReadModelCache()
	writeJSON(writer, http.StatusOK, MessageOnlyResponse{Message: "Certificado raiz instalado e confiado."})
}
