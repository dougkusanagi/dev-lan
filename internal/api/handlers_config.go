// Package api exposes the loopback web server and authenticated API
// shared by the browser GUI, Windows service, CLI and WSL clients.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

func (s *Server) handleConfig(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		view, err := BuildGlobalConfigView(s.service)
		if err != nil {
			writeJSONError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, view)
		return
	}
	if request.Method == http.MethodPost {
		var view GlobalConfigView
		if err := json.NewDecoder(request.Body).Decode(&view); err != nil {
			writeJSONError(writer, http.StatusBadRequest, "configuração global inválida")
			return
		}
		if view.DefaultMode != "" {
			if _, err := domain.ParseMode(view.DefaultMode); err != nil {
				writeJSONError(writer, http.StatusBadRequest, err.Error())
				return
			}
		}
		settings := app.GlobalSettings{
			DefaultMode:       view.DefaultMode,
			WindowsPort:       view.WindowsPort,
			HTTPSPort:         view.HTTPSPort,
			TLSEnabled:        view.TLSEnabled,
			PHPDefaultVersion: view.PHPDefaultVersion,
			Allowlist:         view.Allowlist,
		}
		if _, err := s.service.SaveGlobalSettings(request.Context(), settings); err != nil {
			writeJSONError(writer, http.StatusConflict, err.Error())
			return
		}
		InvalidateReadModelCache(s.service)
		writeJSON(writer, http.StatusOK, MessageOnlyResponse{Message: "Configuração global salva."})
		return
	}
	methodNotAllowed(writer, http.MethodGet+", "+http.MethodPost)
}

func (s *Server) handleConfigExport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	data, err := s.service.ExportConfig()
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(data)
}

func (s *Server) handleConfigImport(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	data, err := io.ReadAll(request.Body)
	if err != nil {
		writeJSONError(writer, http.StatusBadRequest, "ler corpo da requisição: "+err.Error())
		return
	}
	res, err := s.service.ImportConfig(request.Context(), data)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, MessageResponse{Message: "Configuração importada e aplicada com sucesso.", Warnings: res.Warnings})
}

func (s *Server) handleReload(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	result, err := s.service.Reload(request.Context())
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	InvalidateReadModelCache(s.service)
	writeJSON(writer, http.StatusOK, ReloadResponse{Message: "Configurações recarregadas com sucesso.", Result: result})
}

func (s *Server) handlePHPVersions(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	items, err := BuildPHPVersionsView(request.Context(), s.service)
	if err != nil {
		writeJSONError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, items)
}

func (s *Server) handlePHPInstall(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Version    string   `json:"version"`
		Extensions []string `json:"extensions"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	res, err := s.service.PHPInstall(request.Context(), input.Version, input.Extensions)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, MessageResponse{Message: fmt.Sprintf("PHP %s instalado.", input.Version), Warnings: res.Warnings})
	InvalidateColdReadModelCache(s.service)
}

func (s *Server) handlePHPRemove(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	res, err := s.service.PHPRemove(request.Context(), input.Version)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, MessageResponse{Message: fmt.Sprintf("PHP %s removido.", input.Version), Warnings: res.Warnings})
	InvalidateColdReadModelCache(s.service)
}

func (s *Server) handlePHPDefault(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	var input struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSONError(writer, http.StatusBadRequest, "parâmetros inválidos")
		return
	}
	res, err := s.service.SetDefaultPHPVersion(request.Context(), input.Version)
	if err != nil {
		writeJSONError(writer, http.StatusConflict, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, MessageResponse{Message: fmt.Sprintf("PHP padrão alterado para %s.", input.Version), Warnings: res.Warnings})
	InvalidateColdReadModelCache(s.service)
}
