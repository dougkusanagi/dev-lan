package api

import (
	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/metrics"
)

// ErrorResponse is the stable error envelope shared by all HTTP handlers.
type ErrorResponse struct {
	Error string `json:"error"`
}

// MessageResponse is used by mutations that return a human-readable message
// and the warnings produced while applying the change.
type MessageResponse struct {
	Message  string   `json:"message"`
	Warnings []string `json:"warnings"`
}

type MessageOnlyResponse struct {
	Message string `json:"message"`
}

type LogsResponse struct {
	Logs string `json:"logs"`
}

type OutputResponse struct {
	Output string `json:"output"`
}

type ApplyResponse struct {
	Message  string   `json:"message"`
	Status   string   `json:"status"`
	Revision uint64   `json:"revision"`
	Warnings []string `json:"warnings"`
}

type ApplyErrorResponse struct {
	Error    string   `json:"error"`
	Status   string   `json:"status"`
	Revision uint64   `json:"revision"`
	Warnings []string `json:"warnings"`
}

type ReloadResponse struct {
	Message string                  `json:"message"`
	Result  application.ApplyResult `json:"result"`
}

type HealthResponse struct {
	Status  string `json:"status"`
	Version int    `json:"version"`
	Runtime string `json:"runtime"`
}

// CommandResponse is the typed envelope for the thin WSL client protocol.
// Pointers distinguish omitted command-specific fields from values that the
// legacy map response emitted explicitly, including null warnings.
type CommandResponse struct {
	Command       string                              `json:"command"`
	Message       *string                             `json:"message,omitempty"`
	Warnings      *[]string                           `json:"warnings,omitempty"`
	Error         string                              `json:"error,omitempty"`
	Projects      *[]ProjectView                      `json:"projects,omitempty"`
	Status        *SystemStatusView                   `json:"status,omitempty"`
	Topology      *application.TopologyStatus         `json:"topology,omitempty"`
	Caddy         *application.CaddyStatus            `json:"caddy,omitempty"`
	Compatibility *application.WSLCompatibilityReport `json:"compatibility,omitempty"`
	Result        *application.ApplyResult            `json:"result,omitempty"`
	Allocations   *[]application.RouteAllocation      `json:"allocations,omitempty"`
	Paths         *[]string                           `json:"paths,omitempty"`
	Checks        *[]application.DoctorCheck          `json:"checks,omitempty"`
	URL           *string                             `json:"url,omitempty"`
}

// responsePayload is deliberately closed. A handler can only serialize one
// of the explicit transport DTOs or read models registered here; arbitrary
// maps cannot cross the HTTP response boundary.
type responsePayload interface {
	ErrorResponse |
		MessageResponse |
		MessageOnlyResponse |
		LogsResponse |
		OutputResponse |
		ApplyResponse |
		ApplyErrorResponse |
		ReloadResponse |
		HealthResponse |
		ProjectView |
		[]ProjectView |
		SystemStatusView |
		GlobalConfigView |
		OverviewView |
		MutationResult |
		[]PHPVersionView |
		[]DoctorCheckView |
		application.TopologySnapshot |
		*metrics.Snapshot |
		CommandResponse
}
