package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// WSL operation names are deliberately coarse. They are useful for budgets
// and diagnostics without recording command arguments, which may contain
// paths or other sensitive values.
const (
	WSLOperationInstall     = "install"
	WSLOperationReload      = "reload"
	WSLOperationDiscovery   = "discovery"
	WSLOperationStatus      = "status"
	WSLOperationPolling     = "web-poll"
	WSLOperationDoctor      = "doctor"
	WSLOperationAccess      = "access"
	WSLOperationOther       = "other"
	WSLPlaneProtocolVersion = 1
)

type wslOperationContextKey struct{}

// WithWSLOperation attributes all WSLRunner calls made with ctx to a coarse
// operation. It is intentionally context-scoped so nested operations can
// override a web request's label (for example, discovery inside a poll).
func WithWSLOperation(ctx context.Context, operation string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, wslOperationContextKey{}, normalizeWSLOperation(operation))
}

func WSLOperation(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value, ok := ctx.Value(wslOperationContextKey{}).(string); ok {
		return normalizeWSLOperation(value)
	}
	return ""
}

func normalizeWSLOperation(operation string) string {
	operation = strings.TrimSpace(strings.ToLower(operation))
	if operation == "" {
		return WSLPlaneOperationUnclassified
	}
	return operation
}

const WSLPlaneOperationUnclassified = "unclassified"

func wslOperationOr(ctx context.Context, fallback string) string {
	operation := WSLOperation(ctx)
	if operation == "" || operation == WSLPlaneOperationUnclassified {
		return fallback
	}
	return operation
}

// WSLOperationStats is an in-process inventory of wsl.exe invocations. It
// stores aggregate timings only; command arguments are never retained.
type WSLOperationStats struct {
	Calls         uint64        `json:"calls"`
	Failures      uint64        `json:"failures"`
	Canceled      uint64        `json:"canceled"`
	TotalDuration time.Duration `json:"totalDuration"`
	MinDuration   time.Duration `json:"minDuration"`
	MaxDuration   time.Duration `json:"maxDuration"`
}

type WSLStatsSnapshot struct {
	TotalCalls    uint64                       `json:"totalCalls"`
	TotalFailures uint64                       `json:"totalFailures"`
	TotalCanceled uint64                       `json:"totalCanceled"`
	TotalDuration time.Duration                `json:"totalDuration"`
	Operations    map[string]WSLOperationStats `json:"operations"`
}

type WSLStats struct {
	mu            sync.Mutex
	totalCalls    uint64
	totalFailures uint64
	totalCanceled uint64
	totalDuration time.Duration
	operations    map[string]WSLOperationStats
}

func NewWSLStats() *WSLStats {
	return &WSLStats{operations: make(map[string]WSLOperationStats)}
}

func (s *WSLStats) record(operation string, started time.Time, ctx context.Context, err error) {
	if s == nil {
		return
	}
	duration := time.Since(started)
	operation = normalizeWSLOperation(operation)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.operations == nil {
		s.operations = make(map[string]WSLOperationStats)
	}
	item := s.operations[operation]
	item.Calls++
	item.TotalDuration += duration
	if item.MinDuration == 0 || duration < item.MinDuration {
		item.MinDuration = duration
	}
	if duration > item.MaxDuration {
		item.MaxDuration = duration
	}
	if err != nil {
		item.Failures++
		s.totalFailures++
	}
	if ctx != nil && ctx.Err() != nil {
		item.Canceled++
		s.totalCanceled++
	}
	s.operations[operation] = item
	s.totalCalls++
	s.totalDuration += duration
}

func (s *WSLStats) Snapshot() WSLStatsSnapshot {
	if s == nil {
		return WSLStatsSnapshot{Operations: map[string]WSLOperationStats{}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	operations := make(map[string]WSLOperationStats, len(s.operations))
	for key, value := range s.operations {
		operations[key] = value
	}
	return WSLStatsSnapshot{
		TotalCalls:    s.totalCalls,
		TotalFailures: s.totalFailures,
		TotalCanceled: s.totalCanceled,
		TotalDuration: s.totalDuration,
		Operations:    operations,
	}
}

func (s *WSLStats) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalCalls = 0
	s.totalFailures = 0
	s.totalCanceled = 0
	s.totalDuration = 0
	s.operations = make(map[string]WSLOperationStats)
}

// WSLFailureKind is stable enough for callers to decide whether to retry or
// show a setup diagnostic, while the underlying command error remains
// available through errors.Is/As.
type WSLFailureKind string

const (
	WSLFailureUnavailable WSLFailureKind = "unavailable"
	WSLFailureTimeout     WSLFailureKind = "timeout"
	WSLFailureCanceled    WSLFailureKind = "canceled"
	WSLFailureExecution   WSLFailureKind = "execution"
)

type WSLExecutionError struct {
	Operation string
	Kind      WSLFailureKind
	Err       error
}

func (e *WSLExecutionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return fmt.Sprintf("WSL %s falhou (%s)", e.Operation, e.Kind)
	}
	return fmt.Sprintf("WSL %s falhou (%s): %v", e.Operation, e.Kind, e.Err)
}

func (e *WSLExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *WSLExecutionError) Is(target error) bool {
	return e != nil && target == ErrUnavailable && e.Kind == WSLFailureUnavailable
}

func wrapWSLError(operation string, ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	kind := WSLFailureExecution
	if ctx != nil {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			kind = WSLFailureTimeout
		case context.Canceled:
			kind = WSLFailureCanceled
		}
	}
	if kind == WSLFailureExecution && isWSLUnavailable(err) {
		kind = WSLFailureUnavailable
	}
	return &WSLExecutionError{Operation: normalizeWSLOperation(operation), Kind: kind, Err: err}
}

// wsl.exe exists independently of a distribution. Its exit text therefore
// carries the unavailable signal for stopped/missing distributions instead of
// returning os/exec.ErrNotFound. Keep this classifier narrow so a regular
// Linux command-not-found remains an execution failure.
func isWSLUnavailable(err error) bool {
	if errors.Is(err, ErrUnavailable) {
		return true
	}
	message := strings.ToLower(strings.ReplaceAll(err.Error(), "\x00", ""))
	markers := []string{
		"no installed distributions",
		"there is no distribution",
		"distribution with the supplied name",
		"distribution not found",
		"wsl is not installed",
		"wsl service is not running",
		"the wsl service is not running",
		"windows subsystem for linux",
		"wsl/service/wsl_e_",
	}
	for _, marker := range markers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

var (
	ErrExecutionProtocol = errors.New("contrato do execution plane WSL incompatível")
	ErrExecutionConflict = errors.New("request id do execution plane já foi usado com outros argumentos")
)

// WSLExecutionRequest is the Windows-owned contract for one Linux execution.
// The request is sent directly to wsl.exe; there is no Linux-side state or
// daemon implied by this type.
type WSLExecutionRequest struct {
	Version    int      `json:"version"`
	RequestID  string   `json:"requestId"`
	Operation  string   `json:"operation"`
	Command    []string `json:"command"`
	AsRoot     bool     `json:"asRoot,omitempty"`
	Idempotent bool     `json:"idempotent"`
}

type WSLExecutionResponse struct {
	Version   int    `json:"version"`
	RequestID string `json:"requestId"`
	Status    string `json:"status"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorKind string `json:"errorKind,omitempty"`
}

type ExecutionProtocolError struct {
	Expected int
	Received int
}

func (e *ExecutionProtocolError) Error() string {
	return fmt.Sprintf("versão do execution plane incompatível: esperada %d, recebida %d", e.Expected, e.Received)
}

func (e *ExecutionProtocolError) Unwrap() error { return ErrExecutionProtocol }

func (r WSLExecutionRequest) Validate() error {
	if r.Version != WSLPlaneProtocolVersion {
		return &ExecutionProtocolError{Expected: WSLPlaneProtocolVersion, Received: r.Version}
	}
	if strings.TrimSpace(r.RequestID) == "" || strings.ContainsAny(r.RequestID, "\r\n\t") {
		return errors.New("request id do execution plane inválido")
	}
	if normalizeWSLOperation(r.Operation) == WSLPlaneOperationUnclassified {
		return errors.New("operação do execution plane inválida")
	}
	if len(r.Command) == 0 || strings.TrimSpace(r.Command[0]) == "" {
		return errors.New("comando do execution plane vazio")
	}
	for _, argument := range r.Command {
		if strings.ContainsRune(argument, '\x00') {
			return errors.New("argumento do execution plane contém NUL")
		}
	}
	return nil
}

type WSLExecutionCache struct {
	mu      sync.Mutex
	entries map[string]*wslExecutionEntry
}

type wslExecutionEntry struct {
	fingerprint string
	done        chan struct{}
	response    WSLExecutionResponse
	err         error
}

func NewWSLExecutionCache() *WSLExecutionCache {
	return &WSLExecutionCache{entries: make(map[string]*wslExecutionEntry)}
}

func (c *WSLExecutionCache) acquire(requestID, fingerprint string) (*wslExecutionEntry, bool, error) {
	if c == nil {
		return nil, true, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]*wslExecutionEntry)
	}
	if existing, ok := c.entries[requestID]; ok {
		if existing.fingerprint != fingerprint {
			return nil, false, ErrExecutionConflict
		}
		return existing, false, nil
	}
	entry := &wslExecutionEntry{fingerprint: fingerprint, done: make(chan struct{})}
	c.entries[requestID] = entry
	return entry, true, nil
}

func (c *WSLExecutionCache) complete(requestID string, entry *wslExecutionEntry, response WSLExecutionResponse, err error, retain bool) {
	if c == nil || entry == nil {
		return
	}
	c.mu.Lock()
	entry.response = response
	entry.err = err
	close(entry.done)
	if !retain {
		delete(c.entries, requestID)
		c.mu.Unlock()
		return
	}
	// Request IDs are a retry guard, not a second persistent state store. Keep
	// a bounded in-memory window so a long-running controller cannot grow
	// without limit.
	if len(c.entries) > 256 {
		for key, item := range c.entries {
			if item != entry && isClosed(item.done) {
				delete(c.entries, key)
				break
			}
		}
	}
	c.mu.Unlock()
}

func isClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func executionFingerprint(request WSLExecutionRequest) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%d\x00%s\x00%t\x00", request.Version, normalizeWSLOperation(request.Operation), request.AsRoot)
	for _, argument := range request.Command {
		_, _ = hash.Write([]byte(argument))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (r WSLRunner) StatsSnapshot() WSLStatsSnapshot {
	return r.Stats.Snapshot()
}

// Execute implements the versioned, cancelable execution contract. Only
// explicitly idempotent requests are deduplicated by RequestID. The cache is
// owned by this Windows process and contains no Linux/project state.
func (r WSLRunner) Execute(ctx context.Context, request WSLExecutionRequest) (WSLExecutionResponse, error) {
	base := WSLExecutionResponse{Version: WSLPlaneProtocolVersion, RequestID: request.RequestID}
	if err := request.Validate(); err != nil {
		base.Status = "error"
		base.Error = err.Error()
		if protocolErr := (*ExecutionProtocolError)(nil); errors.As(err, &protocolErr) {
			base.ErrorKind = "version_mismatch"
		}
		return base, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		base.Status = "canceled"
		base.Error = err.Error()
		base.ErrorKind = string(executionContextFailureKind(err))
		return base, err
	}

	var entry *wslExecutionEntry
	owner := true
	if request.Idempotent {
		var err error
		entry, owner, err = r.Execution.acquire(request.RequestID, executionFingerprint(request))
		if err != nil {
			base.Status = "error"
			base.Error = err.Error()
			base.ErrorKind = "request_conflict"
			return base, err
		}
		if !owner {
			select {
			case <-entry.done:
				return entry.response, entry.err
			case <-ctx.Done():
				base.Status = "canceled"
				base.Error = ctx.Err().Error()
				base.ErrorKind = string(executionContextFailureKind(ctx.Err()))
				return base, ctx.Err()
			}
		}
	}

	output, err := r.runWithOperation(ctx, request.Operation, request.AsRoot, request.Command...)
	response := base
	if err == nil {
		response.Status = "ok"
		response.Output = output
	} else {
		response.Status = "error"
		response.Error = err.Error()
		response.ErrorKind = wslErrorKind(err, ctx)
		if response.ErrorKind == string(WSLFailureCanceled) || response.ErrorKind == string(WSLFailureTimeout) {
			response.Status = "canceled"
		}
	}
	if request.Idempotent && owner {
		retain := err == nil
		if err != nil {
			kind := wslErrorKind(err, ctx)
			retain = kind != string(WSLFailureUnavailable) && kind != string(WSLFailureTimeout) && kind != string(WSLFailureCanceled)
		}
		r.Execution.complete(request.RequestID, entry, response, err, retain)
	}
	return response, err
}

func executionContextFailureKind(err error) WSLFailureKind {
	if errors.Is(err, context.DeadlineExceeded) {
		return WSLFailureTimeout
	}
	return WSLFailureCanceled
}

func wslErrorKind(err error, ctx context.Context) string {
	var executionErr *WSLExecutionError
	if errors.As(err, &executionErr) {
		return string(executionErr.Kind)
	}
	if ctx != nil && ctx.Err() != nil {
		return string(executionContextFailureKind(ctx.Err()))
	}
	return string(WSLFailureExecution)
}

func (r WSLRunner) RunIdempotent(ctx context.Context, requestID, operation string, args ...string) (string, error) {
	response, err := r.Execute(ctx, WSLExecutionRequest{
		Version:    WSLPlaneProtocolVersion,
		RequestID:  requestID,
		Operation:  operation,
		Command:    append([]string(nil), args...),
		Idempotent: true,
	})
	return response.Output, err
}
