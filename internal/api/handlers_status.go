// Package api exposes the loopback web server and authenticated API
// shared by the browser GUI, Windows service, CLI and WSL clients.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"runtime"
	"strings"
	"time"
)

func (s *Server) handleHealth(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": ProtocolVersion,
		"runtime": runtime.GOOS + "/" + runtime.GOARCH,
	})
}

func (s *Server) handleStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	view, err := BuildStatusView(request.Context(), s.service)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (s *Server) handleTopology(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	writeJSON(writer, http.StatusOK, BuildTopologyView(request.Context(), s.service))
}

func (s *Server) handleOverview(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	filter := request.URL.Query().Get("filter")
	view, err := BuildOverviewView(request.Context(), s.service, filter)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (s *Server) handleOperation(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	id := strings.TrimPrefix(path.Clean(request.URL.Path), "/api/v1/operations/")
	if strings.HasPrefix(request.URL.Path, "/v1/") {
		id = strings.TrimPrefix(path.Clean(request.URL.Path), "/v1/operations/")
	}
	if id == "" || id == "." {
		writeJSONError(writer, http.StatusBadRequest, "operationId ausente")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	result, ok := s.operationResponse(ctx, id)
	if !ok {
		writeJSONError(writer, http.StatusNotFound, "operação não encontrada")
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (s *Server) handleEvents(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeJSONError(writer, http.StatusInternalServerError, "SSE não suportado pelo servidor")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache, no-store")
	writer.Header().Set("Connection", "keep-alive")
	_, _ = writer.Write([]byte(": devlan events\n\n"))
	flusher.Flush()
	updates, stop := s.service.SubscribeOperations(request.Context())
	defer stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case state, open := <-updates:
			if !open {
				return
			}
			result := operationResult(request.Context(), s.service, state)
			eventName := "operation-progress"
			if isTerminalPhase(state.Phase) && state.ProjectName != "" {
				eventName = "project-state-changed"
			}
			data, err := json.Marshal(result)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", eventName, data)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = writer.Write([]byte(": heartbeat\n\n"))
			flusher.Flush()
		}
	}
}
