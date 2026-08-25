// Package telemetry implements an explicit-consent, local queue. Nothing is
// sent unless the user enables telemetry and explicitly configures an HTTPS
// endpoint (or a loopback HTTP endpoint for local deployments).
package telemetry

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Store struct {
	Dir string
}

type Consent struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint,omitempty"`
}

type Event struct {
	Name       string            `json:"name"`
	Timestamp  string            `json:"timestamp"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
var eventPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

var allowedAttributeKeys = map[string]struct{}{
	"channel":   {},
	"component": {},
	"framework": {},
	"mode":      {},
	"operation": {},
	"result":    {},
	"status":    {},
	"version":   {},
}

func NewStore(dir string) Store { return Store{Dir: dir} }

func (s Store) consentPath() string { return filepath.Join(s.Dir, "telemetry.json") }
func (s Store) queuePath() string   { return filepath.Join(s.Dir, "telemetry.queue.jsonl") }

func (s Store) Load() (Consent, error) {
	data, err := os.ReadFile(s.consentPath())
	if errors.Is(err, os.ErrNotExist) {
		return Consent{}, nil
	}
	if err != nil {
		return Consent{}, err
	}
	var consent Consent
	if err := json.Unmarshal(data, &consent); err != nil {
		return Consent{}, fmt.Errorf("ler consentimento de telemetria: %w", err)
	}
	if consent.Enabled {
		if err := ValidateEndpoint(consent.Endpoint); err != nil {
			return Consent{}, err
		}
	}
	return consent, nil
}

func (s Store) SetConsent(enabled bool, endpoint string) error {
	if enabled {
		if err := ValidateEndpoint(endpoint); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(Consent{Enabled: enabled, Endpoint: strings.TrimSpace(endpoint)}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(s.consentPath(), data, 0o600); err != nil {
		return err
	}
	if !enabled {
		if err := os.Remove(s.queuePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func ValidateEndpoint(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.Path == "" && parsed.RawQuery != "" {
		return fmt.Errorf("endpoint de telemetria inválido")
	}
	if parsed.Scheme != "https" {
		if parsed.Scheme != "http" || !isLoopback(parsed.Hostname()) {
			return errors.New("telemetria exige HTTPS; HTTP só é permitido para loopback")
		}
	}
	return nil
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s Store) Record(name string, attributes map[string]string) error {
	consent, err := s.Load()
	if err != nil {
		return err
	}
	if !consent.Enabled {
		return nil
	}
	if !eventPattern.MatchString(name) {
		return fmt.Errorf("evento de telemetria inválido: %q", name)
	}
	event := Event{Name: name, Timestamp: time.Now().UTC().Format(time.RFC3339), Attributes: sanitizeAttributes(attributes)}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.queuePath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}

func sanitizeAttributes(input map[string]string) map[string]string {
	output := make(map[string]string)
	for key, value := range input {
		key = strings.ToLower(strings.TrimSpace(key))
		if !keyPattern.MatchString(key) {
			continue
		}
		if _, allowed := allowedAttributeKeys[key]; !allowed {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n\\/") {
			continue
		}
		output[key] = value
	}
	return output
}

func (s Store) QueueSize() (int, error) {
	data, err := os.ReadFile(s.queuePath())
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return 0, nil
	}
	return len(strings.Split(strings.TrimSpace(string(data)), "\n")), nil
}

func (s Store) Send(ctx context.Context) (int, error) {
	consent, err := s.Load()
	if err != nil {
		return 0, err
	}
	if !consent.Enabled {
		return 0, errors.New("telemetria não está habilitada")
	}
	data, err := os.ReadFile(s.queuePath())
	if errors.Is(err, os.ErrNotExist) || len(strings.TrimSpace(string(data))) == 0 {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	events := make([]Event, 0, len(lines))
	for _, line := range lines {
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return 0, fmt.Errorf("fila de telemetria inválida: %w", err)
		}
		events = append(events, event)
	}
	body, err := json.Marshal(events)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, consent.Endpoint, strings.NewReader(string(body)))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Transport: safeTransport()}).Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("endpoint de telemetria respondeu HTTP %d", response.StatusCode)
	}
	if err := os.WriteFile(s.queuePath(), nil, 0o600); err != nil {
		return 0, err
	}
	return len(events), nil
}

func safeTransport() http.RoundTripper {
	return &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
}
