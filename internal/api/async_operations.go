package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/application"
)

type operationWork func(context.Context) (revision uint64, warnings []string, workErr error)

func (s *Server) currentRevision(_ context.Context) uint64 {
	// The revision is metadata collected after a mutation. A canceled worker
	// context must not erase a revision that was already committed.
	return s.queries.Revision(context.Background())
}

func operationIDOrNew(id string) string {
	if strings.TrimSpace(id) != "" {
		id = strings.TrimSpace(id)
		if len(id) > 96 {
			return id[:96]
		}
		return id
	}
	return application.NewOperationID()
}

func (s *Server) startAsyncOperation(writer http.ResponseWriter, request *http.Request, operation, project, requestedID string, timeout time.Duration, work operationWork) {
	id := operationIDOrNew(requestedID)
	state, existed, err := s.commands.BeginOperation(id, operation, project)
	if err != nil {
		writeJSONError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if !existed {
		s.commands.SetOperationTransport(id, "http")
		go s.runAsyncOperation(id, operation, project, timeout, work)
	}
	writeJSON(writer, http.StatusAccepted, s.operationResult(request.Context(), state))
}

func (s *Server) runAsyncOperation(id, operation, project string, timeout time.Duration, work operationWork) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	phase := "applying"
	if operation == "start" || operation == "restart" {
		phase = "starting"
	} else if operation == "stop" {
		phase = "stopping"
	}
	s.commands.UpdateOperation(id, phase, phase, 0, nil, nil, nil)
	revision, warnings, err := work(ctx)
	terminal := "ready"
	if operation == "stop" {
		terminal = "stopped"
	}
	if err != nil {
		terminal = "failed"
		if strings.Contains(strings.ToLower(err.Error()), "rolled_back") || errors.Is(err, context.DeadlineExceeded) {
			// A timeout after persistence is ambiguous. The follow-up operation
			// read still carries revision/warnings and the overview is revalidated.
			terminal = "rolled_back"
		}
	}
	if operation == "start" || operation == "stop" || operation == "restart" {
		s.InvalidateHotReadModelCache()
	} else {
		s.InvalidateReadModelCache()
	}
	state := s.commands.UpdateOperation(id, terminal, terminal, revision, nil, warnings, err)
	if state.OperationID == "" {
		return
	}
}

func (s *Server) operationResponse(ctx context.Context, id string) (MutationResult, bool) {
	state, ok := s.queries.Operation(id)
	if !ok {
		return MutationResult{}, false
	}
	return s.operationResult(ctx, state), true
}
