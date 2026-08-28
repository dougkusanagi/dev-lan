package app

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/application"
)

type operationSubscriber struct {
	channel chan OperationState
}

const operationRetention = 15 * time.Minute

// NewOperationID is shared by transports so a client can safely retry a
// mutation without accidentally starting it twice.
func NewOperationID() string {
	return application.NewOperationID()
}

func normalizeOperationID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 96 {
		return id[:96]
	}
	return id
}

// BeginOperation records an accepted operation. The bool is true when the ID
// already existed; callers must return the existing state and never rerun it.
func (a *App) BeginOperation(id, operation, project string) (OperationState, bool, error) {
	id = normalizeOperationID(id)
	if id == "" {
		id = NewOperationID()
	}
	if strings.ContainsAny(id, "\r\n") || strings.TrimSpace(operation) == "" {
		return OperationState{}, false, errors.New("identificador de operação inválido")
	}
	now := time.Now()
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	if a.operations == nil {
		a.operations = make(map[string]OperationState)
	}
	for key, item := range a.operations {
		if !item.FinishedAt.IsZero() && now.Sub(item.UpdatedAt) > operationRetention {
			delete(a.operations, key)
		}
	}
	if current, ok := a.operations[id]; ok {
		if current.Operation != strings.TrimSpace(operation) || current.ProjectName != strings.TrimSpace(project) {
			return OperationState{}, false, errors.New("operationId já está associado a outra operação")
		}
		return cloneOperationState(current), true, nil
	}
	state := OperationState{
		OperationID: id,
		Operation:   strings.TrimSpace(operation),
		ProjectName: strings.TrimSpace(project),
		Phase:       "accepted",
		Status:      "accepted",
		StartedAt:   now,
		UpdatedAt:   now,
		PhaseMs:     make(map[string]int64),
	}
	a.operations[id] = state
	a.publishOperationLocked(state)
	return cloneOperationState(state), false, nil
}

func (a *App) UpdateOperation(id, phase, status string, revision uint64, projectState json.RawMessage, warnings []string, operationErr error) OperationState {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	state, ok := a.operations[normalizeOperationID(id)]
	if !ok {
		return OperationState{}
	}
	if isTerminalOperationPhase(state.Phase) {
		return cloneOperationState(state)
	}
	now := time.Now()
	if !state.UpdatedAt.IsZero() && state.Phase != "" {
		elapsed := now.Sub(state.UpdatedAt).Milliseconds()
		if state.PhaseMs == nil {
			state.PhaseMs = make(map[string]int64)
		}
		state.PhaseMs[state.Phase] += elapsed
	}
	state.Phase = strings.TrimSpace(phase)
	if status != "" {
		state.Status = strings.TrimSpace(status)
	} else {
		state.Status = state.Phase
	}
	if revision > state.Revision {
		state.Revision = revision
	}
	if len(projectState) > 0 {
		state.ProjectState = append(json.RawMessage(nil), projectState...)
	}
	if warnings != nil {
		state.Warnings = append([]string(nil), warnings...)
	}
	if operationErr != nil {
		state.Error = operationErr.Error()
	}
	state.UpdatedAt = now
	if isTerminalOperationPhase(state.Phase) {
		state.FinishedAt = now
	}
	a.operations[state.OperationID] = state
	a.publishOperationLocked(state)
	if state.FinishedAt.IsZero() == false {
		attributes := map[string]string{
			"operation":    state.Operation,
			"result":       state.Status,
			"status":       state.Phase,
			"operation_id": state.OperationID,
			"duration_ms":  strconv.FormatInt(operationDurationMs(state), 10),
		}
		if state.ProjectName != "" {
			attributes["component"] = "project"
		}
		if state.Transport != "" {
			attributes["transport"] = state.Transport
		}
		a.recordTelemetry("operation.completed", attributes)
	}
	return cloneOperationState(state)
}

// SetOperationTransport adds the transport dimension to the terminal timing
// event without making the core operation registry depend on HTTP or Wails.
func (a *App) SetOperationTransport(id, transport string) {
	transport = strings.TrimSpace(strings.ToLower(transport))
	if transport == "" {
		return
	}
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	state, ok := a.operations[normalizeOperationID(id)]
	if !ok {
		return
	}
	state.Transport = transport
	a.operations[state.OperationID] = state
}

func isTerminalOperationPhase(phase string) bool {
	switch phase {
	case "ready", "stopped", "failed", "rolled_back":
		return true
	default:
		return false
	}
}

func operationDurationMs(state OperationState) int64 {
	if state.StartedAt.IsZero() {
		return 0
	}
	end := state.FinishedAt
	if end.IsZero() {
		end = state.UpdatedAt
	}
	return end.Sub(state.StartedAt).Milliseconds()
}

func (a *App) Operation(id string) (OperationState, bool) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	state, ok := a.operations[normalizeOperationID(id)]
	return cloneOperationState(state), ok
}

// SubscribeOperations is used by the loopback SSE endpoint. The channel is
// buffered and updates are coalesced by the consumer, so a slow browser cannot
// block a mutation or the WSL control plane.
func (a *App) SubscribeOperations(ctx context.Context) (<-chan OperationState, func()) {
	channel := make(chan OperationState, 16)
	a.operationMu.Lock()
	if a.operationSubscribers == nil {
		a.operationSubscribers = make(map[*operationSubscriber]struct{})
	}
	subscriber := &operationSubscriber{channel: channel}
	a.operationSubscribers[subscriber] = struct{}{}
	a.operationMu.Unlock()

	stop := func() {
		a.operationMu.Lock()
		if _, ok := a.operationSubscribers[subscriber]; ok {
			delete(a.operationSubscribers, subscriber)
			close(channel)
		}
		a.operationMu.Unlock()
	}
	if ctx != nil {
		go func() {
			<-ctx.Done()
			stop()
		}()
	}
	return channel, stop
}

func (a *App) publishOperationLocked(state OperationState) {
	for subscriber := range a.operationSubscribers {
		select {
		case subscriber.channel <- cloneOperationState(state):
		default:
			// A later snapshot supersedes an unread progress update.
		}
	}
}

func cloneOperationState(state OperationState) OperationState {
	state.Warnings = append([]string(nil), state.Warnings...)
	if state.PhaseMs != nil {
		state.PhaseMs = make(map[string]int64, len(state.PhaseMs))
		for key, value := range state.PhaseMs {
			state.PhaseMs[key] = value
		}
	}
	return state
}
