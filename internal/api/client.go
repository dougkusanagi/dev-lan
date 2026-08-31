// Package api exposes the loopback web server and authenticated API
// shared by the browser GUI, Windows service, CLI and WSL clients.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/application"
)

type Client struct {
	EndpointFile string
	TokenFile    string
	HTTPClient   *http.Client
}

// NewClient creates a local API client from the application-owned discovery
// files. CLI and tray callers never need direct access to persistent state.
func NewClient(service *app.App) Client {
	return NewClientFromFiles(service.APIEndpointFiles())
}

// NewClientForDataDir is for the thin WSL client, which only discovers the
// authenticated Windows endpoint and never opens a controller or state store.
func NewClientForDataDir(dataDir string) Client {
	return Client{
		EndpointFile: filepath.Join(dataDir, "api.endpoint.json"),
		TokenFile:    filepath.Join(dataDir, "api.token"),
	}
}

func NewClientFromFiles(files app.APIEndpointFiles) Client {
	return Client{EndpointFile: files.Endpoint, TokenFile: files.Token}
}

func (c Client) Command(ctx context.Context, command string, args []string) (CommandResponse, error) {
	body, err := json.Marshal(commandRequest{Command: command, Args: args})
	if err != nil {
		return CommandResponse{}, err
	}
	response, err := c.Do(ctx, http.MethodPost, "/v1/command", strings.NewReader(string(body)))
	if err != nil {
		return CommandResponse{}, err
	}
	defer response.Body.Close()
	var payload CommandResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return CommandResponse{}, err
	}
	if response.StatusCode >= 400 {
		if payload.Error != "" {
			return CommandResponse{}, errors.New(payload.Error)
		}
		return CommandResponse{}, fmt.Errorf("API local respondeu HTTP %d", response.StatusCode)
	}
	return payload, nil
}

// Reload asks the already-running controller to reconcile state and runtime.
// Keeping this operation on the API is important: a CLI command must not open
// a second App/Store while the user-session controller is active.
func (c Client) Reload(ctx context.Context) (application.ApplyResult, error) {
	response, err := c.Do(ctx, http.MethodPost, "/v1/reload", strings.NewReader("{}"))
	if err != nil {
		return application.ApplyResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		var payload ErrorResponse
		if decodeErr := json.NewDecoder(response.Body).Decode(&payload); decodeErr == nil && payload.Error != "" {
			return application.ApplyResult{}, fmt.Errorf("API local respondeu HTTP %d: %s", response.StatusCode, payload.Error)
		}
		return application.ApplyResult{}, fmt.Errorf("API local respondeu HTTP %d", response.StatusCode)
	}
	var payload ReloadResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return application.ApplyResult{}, err
	}
	return payload.Result, nil
}

func (c Client) Do(ctx context.Context, method, route string, body io.Reader) (*http.Response, error) {
	if !strings.HasPrefix(route, "/v1/") && !strings.HasPrefix(route, "/api/v1/") {
		return nil, fmt.Errorf("rota da API local inválida: %q", route)
	}
	endpoint, err := ReadEndpoint(c.EndpointFile, c.TokenFile)
	if err != nil {
		return nil, err
	}
	token, err := readToken(endpoint.TokenFile)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://"+endpoint.Address+route, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(request)
}
