package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Channel string

const (
	Stable  Channel = "stable"
	Preview Channel = "preview"
)

type Manifest struct {
	Version string  `json:"version"`
	Channel Channel `json:"channel"`
	URL     string  `json:"url"`
	SHA256  string  `json:"sha256"`
	Size    int64   `json:"size,omitempty"`
}

func ParseChannel(value string) (Channel, error) {
	channel := Channel(strings.ToLower(strings.TrimSpace(value)))
	if channel != Stable && channel != Preview {
		return "", fmt.Errorf("canal de atualização inválido %q (use stable ou preview)", value)
	}
	return channel, nil
}

func FetchManifest(ctx context.Context, client *http.Client, rawURL string, channel Channel) (Manifest, error) {
	if err := validateURL(rawURL); err != nil {
		return Manifest{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Manifest{}, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return Manifest{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Manifest{}, fmt.Errorf("manifesto respondeu HTTP %d", response.StatusCode)
	}
	var manifest Manifest
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("ler manifesto: %w", err)
	}
	if err := manifest.Validate(channel); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate(expectedChannel Channel) error {
	if expectedChannel != Stable && expectedChannel != Preview {
		return fmt.Errorf("canal esperado inválido: %q", expectedChannel)
	}
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("manifesto sem versão")
	}
	if m.Channel != expectedChannel {
		return fmt.Errorf("manifesto do canal %q não corresponde ao canal solicitado %q", m.Channel, expectedChannel)
	}
	if err := validateURL(m.URL); err != nil {
		return fmt.Errorf("URL do artefato: %w", err)
	}
	checksum := strings.ToLower(strings.TrimSpace(m.SHA256))
	if len(checksum) != sha256.Size*2 {
		return errors.New("manifesto sem SHA-256 válido")
	}
	if _, err := hex.DecodeString(checksum); err != nil {
		return fmt.Errorf("SHA-256 inválido: %w", err)
	}
	return nil
}

// DownloadVerified stages an update only after the downloaded bytes match the
// manifest. Replacing the running executable is intentionally left to the
// platform installer/service; this function never executes unverified data.
func DownloadVerified(ctx context.Context, client *http.Client, manifest Manifest, channel Channel, target string) error {
	if err := manifest.Validate(channel); err != nil {
		return err
	}
	if strings.TrimSpace(target) == "" {
		return errors.New("destino do update não pode ser vazio")
	}
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifest.URL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("artefato respondeu HTTP %d", response.StatusCode)
	}
	if manifest.Size > 0 && response.ContentLength >= 0 && response.ContentLength != manifest.Size {
		return fmt.Errorf("tamanho do artefato divergente: esperado %d, recebido %d", manifest.Size, response.ContentLength)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".devlan-update-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	hash := sha256.New()
	writer := io.MultiWriter(temporary, hash)
	limit := io.Reader(response.Body)
	if manifest.Size > 0 {
		limit = io.LimitReader(response.Body, manifest.Size+1)
	}
	written, err := io.Copy(writer, limit)
	if err != nil {
		_ = temporary.Close()
		return err
	}
	if manifest.Size > 0 && written != manifest.Size {
		_ = temporary.Close()
		return fmt.Errorf("tamanho do artefato divergente: esperado %d, recebido %d", manifest.Size, written)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != strings.ToLower(strings.TrimSpace(manifest.SHA256)) {
		_ = temporary.Close()
		return fmt.Errorf("checksum do update inválido: esperado %s, obtido %s", manifest.SHA256, actual)
	}
	if err := temporary.Chmod(0o700); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, target); err == nil {
		return nil
	} else if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return err
	}
	return os.Rename(temporaryName, target)
}

func validateURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return errors.New("URL inválida")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && isLoopback(parsed.Hostname()) {
		return nil
	}
	return errors.New("updates exigem HTTPS; HTTP só é permitido para loopback")
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// IsNewer compares the numeric components of versions and ignores a leading
// v. It is deliberately conservative for non-semver development labels.
func IsNewer(current, candidate string) bool {
	currentParts, currentOK := numericVersion(strings.TrimPrefix(strings.TrimSpace(current), "v"))
	candidateParts, candidateOK := numericVersion(strings.TrimPrefix(strings.TrimSpace(candidate), "v"))
	if !currentOK || !candidateOK {
		return false
	}
	for index := 0; index < 3; index++ {
		if currentParts[index] != candidateParts[index] {
			return candidateParts[index] > currentParts[index]
		}
	}
	return false
}

func numericVersion(value string) ([3]int, bool) {
	var result [3]int
	parts := strings.SplitN(value, ".", 4)
	if len(parts) < 3 {
		return result, false
	}
	for index := 0; index < 3; index++ {
		part := parts[index]
		for position, character := range part {
			if character < '0' || character > '9' {
				if index == 2 && position > 0 {
					part = part[:position]
					break
				}
				return result, false
			}
		}
		if part == "" {
			return result, false
		}
		parsed, err := strconv.Atoi(part)
		if err != nil {
			return result, false
		}
		result[index] = parsed
	}
	return result, true
}
