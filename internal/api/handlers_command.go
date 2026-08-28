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
)

type commandRequest struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

func (s *Server) handleCommand(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input commandRequest
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "comando WSL inválido")
		return
	}
	input.Command = strings.ToLower(strings.TrimSpace(input.Command))
	for i := range input.Args {
		input.Args[i] = strings.TrimSpace(input.Args[i])
	}
	ctx, cancel := context.WithTimeout(request.Context(), 45*time.Second)
	defer cancel()

	response := CommandResponse{Command: input.Command}
	setMessage := func(message string) { response.Message = &message }
	setWarnings := func(warnings []string) { response.Warnings = &warnings }
	switch input.Command {
	case "link":
		if len(input.Args) != 2 {
			writeJSONError(writer, http.StatusBadRequest, "uso: link NAME PATH")
			return
		}
		project, result, err := s.commands.LinkProject(ctx, application.LinkProjectCommand{Name: input.Args[0], Path: input.Args[1]})
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		setMessage(fmt.Sprintf("Projeto %s registrado: %s", project.Name, project.Path))
		setWarnings(result.Warnings)
	case "unlink":
		if len(input.Args) != 1 {
			writeJSONError(writer, http.StatusBadRequest, "uso: unlink NAME")
			return
		}
		project, result, err := s.commands.UnlinkProject(ctx, application.UnlinkProjectCommand{Name: input.Args[0]})
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		setMessage(fmt.Sprintf("Projeto %s removido do registro.", project.Name))
		setWarnings(result.Warnings)
	case "park":
		if len(input.Args) == 2 && (input.Args[0] == "ignore" || input.Args[0] == "unignore") {
			var result application.ApplyResult
			var err error
			if input.Args[0] == "ignore" {
				result, err = s.commands.IgnoreProject(ctx, application.IgnoreProjectCommand{Selector: input.Args[1]})
			} else {
				result, err = s.commands.UnignoreProject(ctx, application.UnignoreProjectCommand{Path: input.Args[1]})
			}
			if err != nil {
				writeJSONError(writer, http.StatusConflict, err.Error())
				return
			}
			setMessage(fmt.Sprintf("Projeto %s: %s", input.Args[0], input.Args[1]))
			setWarnings(result.Warnings)
			break
		}
		if len(input.Args) != 1 {
			writeJSONError(writer, http.StatusBadRequest, "uso: park PATH | park ignore NAME|PATH | park unignore PATH")
			return
		}
		park, result, err := s.commands.ParkDirectory(ctx, application.ParkDirectoryCommand{Path: input.Args[0]})
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		setMessage(fmt.Sprintf("Diretório estacionado: %s", park.Path))
		setWarnings(result.Warnings)
	case "unpark":
		if len(input.Args) != 1 {
			writeJSONError(writer, http.StatusBadRequest, "uso: unpark PATH")
			return
		}
		park, result, err := s.commands.UnparkDirectory(ctx, application.UnparkDirectoryCommand{Path: input.Args[0]})
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		setMessage(fmt.Sprintf("Diretório removido dos estacionados: %s", park.Path))
		setWarnings(result.Warnings)
	case "links":
		if len(input.Args) > 1 {
			writeJSONError(writer, http.StatusBadRequest, "uso: links [FILTRO]")
			return
		}
		filter := ""
		if len(input.Args) == 1 {
			filter = input.Args[0]
		}
		views, err := s.BuildProjectViews(ctx, filter)
		if err != nil {
			writeJSONError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		response.Projects = &views
	case "status":
		if len(input.Args) != 0 {
			writeJSONError(writer, http.StatusBadRequest, "uso: status")
			return
		}
		statusView, err := s.BuildStatusView(ctx)
		if err != nil {
			writeJSONError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		response.Status = &statusView
	case "topology":
		if len(input.Args) > 1 || (len(input.Args) == 1 && input.Args[0] != "status" && input.Args[0] != "check") {
			writeJSONError(writer, http.StatusBadRequest, "uso: topology [status|check]")
			return
		}
		topology := s.service.CaddyTopologyStatus(ctx)
		caddy := s.service.CaddyStatus(ctx)
		response.Topology = &topology
		response.Caddy = &caddy
		if len(input.Args) == 1 && input.Args[0] == "check" {
			compatibility := s.service.WSLCompatibility(ctx)
			response.Compatibility = &compatibility
		}
	case "reload":
		if len(input.Args) != 0 {
			writeJSONError(writer, http.StatusBadRequest, "uso: reload")
			return
		}
		result, err := s.service.Reload(ctx)
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		response.Result = &result
		setMessage("Configurações recarregadas.")
	case "route":
		if len(input.Args) > 0 && input.Args[0] == "allocations" {
			if len(input.Args) == 1 {
				allocations, err := s.service.RouteAllocations(ctx)
				if err != nil {
					writeJSONError(writer, http.StatusInternalServerError, err.Error())
					return
				}
				response.Allocations = &allocations
				break
			}
			if len(input.Args) == 2 && input.Args[1] == "prune" {
				paths, result, err := s.service.PruneRouteAllocations(ctx, false)
				if err != nil {
					writeJSONError(writer, http.StatusConflict, err.Error())
					return
				}
				response.Paths = &paths
				response.Result = &result
				break
			}
			if len(input.Args) == 3 && input.Args[1] == "prune" && input.Args[2] == "--dry-run" {
				paths, result, err := s.service.PruneRouteAllocations(ctx, true)
				if err != nil {
					writeJSONError(writer, http.StatusInternalServerError, err.Error())
					return
				}
				response.Paths = &paths
				response.Result = &result
				break
			}
			writeJSONError(writer, http.StatusBadRequest, "uso: route allocations [prune [--dry-run]]")
			return
		}
		if len(input.Args) != 3 || input.Args[1] != "--port" {
			writeJSONError(writer, http.StatusBadRequest, "uso: route NAME --port auto|PORT")
			return
		}
		var port *int
		if input.Args[2] != "auto" {
			parsed, parseErr := strconv.Atoi(input.Args[2])
			if parseErr != nil || parsed < 1024 || parsed > 65535 {
				writeJSONError(writer, http.StatusBadRequest, "porta inválida")
				return
			}
			port = &parsed
		}
		result, err := s.service.SetRoutePort(ctx, input.Args[0], port)
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		response.Result = &result
		setWarnings(result.Warnings)
		setMessage("Porta LAN atualizada.")
	case "doctor":
		if len(input.Args) > 1 {
			writeJSONError(writer, http.StatusBadRequest, "uso: doctor [NAME]")
			return
		}
		name := ""
		if len(input.Args) == 1 {
			name = input.Args[0]
		}
		checks, err := s.service.Doctor(ctx, name)
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		response.Checks = &checks
	case "open":
		if len(input.Args) > 1 {
			writeJSONError(writer, http.StatusBadRequest, "uso: open [NAME]")
			return
		}
		name := ""
		if len(input.Args) == 1 {
			name = input.Args[0]
		}
		if name == "" {
			writeJSONError(writer, http.StatusBadRequest, "open no WSL exige NAME")
			return
		}
		url, err := s.service.Open(ctx, name)
		if err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		response.URL = &url
		setMessage(url)
	default:
		writeJSONError(writer, http.StatusNotFound, "comando não permitido pelo protocolo WSL: "+input.Command)
		return
	}
	writeJSON(writer, http.StatusOK, response)
}
