// Package api exposes the loopback web server and authenticated API
// shared by the browser GUI, Windows service, CLI and WSL clients.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

func (s *Server) handleProjects(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	filter := request.URL.Query().Get("filter")
	views, err := s.BuildProjectViews(request.Context(), filter)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, views)
}

func (s *Server) handleProjectLogs(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	name := request.URL.Query().Get("name")
	linesStr := request.URL.Query().Get("lines")
	lines := 100
	if l, err := strconv.Atoi(linesStr); err == nil && l > 0 {
		lines = l
	}
	devLogs, err := s.queries.ProjectDevLogs(request.Context(), name, lines)
	if err == nil && strings.TrimSpace(devLogs) != "" {
		writeJSON(writer, http.StatusOK, LogsResponse{Logs: devLogs})
		return
	}
	globalLogs, _ := s.queries.Logs("devlan")
	writeJSON(writer, http.StatusOK, LogsResponse{Logs: fmt.Sprintf("Nenhum log de servidor dev para %s.\n\nLogs do DevLAN:\n%s", name, globalLogs)})
}

func (s *Server) handleProjectLink(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	proj, res, err := s.commands.LinkProject(request.Context(), application.LinkProjectCommand{Name: input.Name, Path: input.Path})
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, MessageResponse{Message: fmt.Sprintf("Projeto %s vinculado.", proj.Name), Warnings: res.Warnings})
	s.InvalidateReadModelCache()
}

func (s *Server) handleProjectUnlink(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	proj, res, err := s.commands.UnlinkProject(request.Context(), application.UnlinkProjectCommand{Name: input.Name})
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, MessageResponse{Message: fmt.Sprintf("Projeto %s desvinculado.", proj.Name), Warnings: res.Warnings})
	s.InvalidateReadModelCache()
}

func (s *Server) handleProjectHide(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	res, err := s.commands.IgnoreProject(request.Context(), application.IgnoreProjectCommand{Selector: input.Name})
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, MessageResponse{Message: fmt.Sprintf("Projeto %s ocultado.", input.Name), Warnings: res.Warnings})
	s.InvalidateReadModelCache()
}

func (s *Server) handleProjectUnhide(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	res, err := s.commands.UnignoreProject(request.Context(), application.UnignoreProjectCommand{Path: input.Path})
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, MessageResponse{Message: fmt.Sprintf("Projeto %s restaurado.", input.Path), Warnings: res.Warnings})
	s.InvalidateReadModelCache()
}

func (s *Server) handleProjectConfig(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var update ProjectConfigUpdate
	if err := json.NewDecoder(request.Body).Decode(&update); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "configuração de projeto inválida")
		return
	}
	ctx := request.Context()
	if update.TLSEnabled != nil && update.Mode == "" && update.PHPVersion == "" && update.PHPPreset == "" &&
		update.RoutePort == nil && !update.RoutePortAuto && update.StaticDir == "" && update.DevCommand == "" && update.DevPort == 0 {
		target := *update.TLSEnabled
		s.startAsyncOperation(writer, request, "tls", update.Name, update.OperationID, 90*time.Second,
			func(workCtx context.Context) (uint64, []string, error) {
				result, _, err := s.commands.SetProjectTLS(workCtx, update.Name, target)
				return result.Revision, result.Warnings, err
			})
		return
	}
	if update.Mode != "" {
		m, err := domain.ParseMode(update.Mode)
		if err != nil {
			writeJSONError(writer, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := s.commands.SetProjectMode(ctx, application.SetProjectModeCommand{Name: update.Name, Mode: &m}); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	if update.TLSEnabled != nil {
		if _, _, err := s.commands.SetProjectTLS(ctx, update.Name, *update.TLSEnabled); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	if update.PHPVersion != "" {
		if _, err := s.commands.SetProjectPHPVersion(ctx, update.Name, update.PHPVersion); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	if update.PHPPreset != "" {
		if _, err := s.commands.SetProjectPHPPreset(ctx, update.Name, update.PHPPreset); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	if update.RoutePortAuto || update.RoutePort != nil {
		var port *int
		if !update.RoutePortAuto {
			port = update.RoutePort
		}
		result, err := s.commands.SetRoutePort(ctx, update.Name, port)
		if err != nil {
			writeApplyError(writer, http.StatusConflict, result, err)
			return
		}
		writeJSON(writer, http.StatusOK, ApplyResponse{
			Message: "Configuração do projeto salva.", Status: result.Status,
			Revision: result.Revision, Warnings: result.Warnings,
		})
		s.InvalidateReadModelCache()
		return
	}
	if update.StaticDir != "" {
		if _, err := s.commands.SetProjectStaticDir(ctx, update.Name, update.StaticDir); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	if update.DevCommand != "" {
		if _, err := s.commands.SetProjectDevCommand(ctx, update.Name, update.DevCommand); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	if update.DevPort > 0 {
		if _, err := s.commands.SetProjectDevPort(ctx, update.Name, update.DevPort); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
	}
	s.InvalidateReadModelCache()
	writeJSON(writer, http.StatusOK, MessageOnlyResponse{Message: "Configuração do projeto salva."})
}

func writeApplyError(writer http.ResponseWriter, status int, result application.ApplyResult, err error) {
	writeJSON(writer, status, ApplyErrorResponse{
		Error: err.Error(), Status: result.Status,
		Revision: result.Revision, Warnings: result.Warnings,
	})
}

func (s *Server) handleProjectStart(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name        string `json:"name"`
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	s.startAsyncOperation(writer, request, "start", input.Name, input.OperationID, 90*time.Second,
		func(ctx context.Context) (uint64, []string, error) {
			err := s.commands.StartDev(ctx, input.Name)
			return s.currentRevision(ctx), nil, err
		})
}

func (s *Server) handleProjectStop(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name        string `json:"name"`
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	s.startAsyncOperation(writer, request, "stop", input.Name, input.OperationID, 45*time.Second,
		func(ctx context.Context) (uint64, []string, error) {
			err := s.commands.StopDev(ctx, input.Name)
			return s.currentRevision(ctx), nil, err
		})
}

func (s *Server) handleProjectRestart(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name        string `json:"name"`
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	s.startAsyncOperation(writer, request, "restart", input.Name, input.OperationID, 90*time.Second,
		func(ctx context.Context) (uint64, []string, error) {
			err := s.commands.RestartDev(ctx, input.Name)
			return s.currentRevision(ctx), nil, err
		})
}

func (s *Server) handleProjectBuild(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	out, err := s.commands.BuildProject(request.Context(), input.Name)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, OutputResponse{Output: out})
}

func (s *Server) handleProjectDeps(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	out, err := s.commands.InstallDeps(request.Context(), input.Name)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, OutputResponse{Output: out})
}

func (s *Server) handleProjectTLS(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Name        string `json:"name"`
		Enabled     bool   `json:"enabled"`
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	s.startAsyncOperation(writer, request, "tls", input.Name, input.OperationID, 90*time.Second,
		func(ctx context.Context) (uint64, []string, error) {
			result, _, err := s.commands.SetProjectTLS(ctx, input.Name, input.Enabled)
			return result.Revision, result.Warnings, err
		})
}

func (s *Server) handlePark(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	park, res, err := s.commands.ParkDirectory(request.Context(), application.ParkDirectoryCommand{Path: input.Path})
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, MessageResponse{Message: fmt.Sprintf("Pasta %s estacionada.", park.Path), Warnings: res.Warnings})
	s.InvalidateReadModelCache()
}

func (s *Server) handleUnpark(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	park, res, err := s.commands.UnparkDirectory(request.Context(), application.UnparkDirectoryCommand{Path: input.Path})
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, MessageResponse{Message: fmt.Sprintf("Pasta %s desestacionada.", park.Path), Warnings: res.Warnings})
	s.InvalidateReadModelCache()
}
