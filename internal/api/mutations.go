package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/application"
)

func operationResult(ctx context.Context, service *app.App, queries *application.Queries, cache *ReadModelCache, state app.OperationState) MutationResult {
	result := MutationResult{
		OperationID: state.OperationID,
		Operation:   state.Operation,
		Phase:       state.Phase,
		Status:      state.Status,
		Revision:    state.Revision,
		Warnings:    append([]string(nil), state.Warnings...),
		Error:       state.Error,
		StartedAt:   formatTime(state.StartedAt),
		UpdatedAt:   formatTime(state.UpdatedAt),
		DurationMs:  operationDurationMs(state),
		PhaseMs:     state.PhaseMs,
	}
	if !state.FinishedAt.IsZero() {
		result.ObservedAt = formatTime(state.FinishedAt)
	} else {
		result.ObservedAt = formatTime(state.UpdatedAt)
	}
	if len(state.ProjectState) > 0 {
		var project ProjectView
		if err := json.Unmarshal(state.ProjectState, &project); err == nil {
			result.ProjectState = &project
		}
	}
	// A terminal result always tries to include a fresh authoritative project
	// view. If the read model is temporarily unavailable, the operation itself
	// remains valid and the frontend will retry through the overview coordinator.
	if result.ProjectState == nil && service != nil && state.ProjectName != "" && isTerminalPhase(state.Phase) {
		if views, err := buildProjectViews(ctx, service, queries, cache, state.ProjectName); err == nil {
			for _, view := range views {
				if view.Name == state.ProjectName {
					result.ProjectState = &view
					if view.Revision > result.Revision {
						result.Revision = view.Revision
					}
					break
				}
			}
		}
	}
	if result.ProjectState != nil && result.Revision == 0 {
		result.Revision = result.ProjectState.Revision
	}
	return result
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func operationDurationMs(state app.OperationState) int64 {
	if state.StartedAt.IsZero() {
		return 0
	}
	end := state.FinishedAt
	if end.IsZero() {
		end = state.UpdatedAt
	}
	return end.Sub(state.StartedAt).Milliseconds()
}

func isTerminalPhase(phase string) bool {
	switch phase {
	case "ready", "stopped", "failed", "rolled_back":
		return true
	default:
		return false
	}
}

func acceptedResult(state app.OperationState) MutationResult {
	return operationResult(context.Background(), nil, nil, nil, state)
}

func (s *Server) operationResult(ctx context.Context, state app.OperationState) MutationResult {
	return operationResult(ctx, s.service, s.queries, s.readModelCache, state)
}

// BuildOperationResult is exported for the Wails adapter, which shares the
// same operation registry but has no HTTP request handler.
func BuildOperationResult(ctx context.Context, service *app.App, state app.OperationState) MutationResult {
	return operationResult(ctx, service, application.NewQueries(service), NewReadModelCache(), state)
}

func (s *Server) BuildOperationResult(ctx context.Context, state app.OperationState) MutationResult {
	return operationResult(ctx, s.service, s.queries, s.readModelCache, state)
}
