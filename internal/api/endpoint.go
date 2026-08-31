// Package api exposes the loopback web server and authenticated API
// shared by the browser GUI, Windows service, CLI and WSL clients.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

func currentUser() string {
	if current, err := user.Current(); err == nil && current.Username != "" {
		return current.Username
	}
	return "unknown"
}

func ensureToken(path string) (string, error) {
	if token, err := readToken(path); err == nil {
		return token, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	bytes := make([]byte, tokenSize)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("gerar token da API local: %w", err)
	}
	token := hex.EncodeToString(bytes)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return readToken(path)
		}
		return "", fmt.Errorf("criar token da API local: %w", err)
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("gravar token da API local: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	_ = os.Chmod(path, 0o600)
	return token, nil
}

func readToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if len(token) != tokenSize*2 {
		return "", fmt.Errorf("token da API local inválido")
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", fmt.Errorf("token da API local inválido: %w", err)
	}
	return token, nil
}

func writeEndpoint(path string, endpoint Endpoint) error {
	data, err := json.MarshalIndent(endpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar endpoint da API: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".devlan-api-endpoint-*.tmp")
	if err != nil {
		return fmt.Errorf("criar endpoint temporário: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(temporaryName, path)
}

func ReadEndpoint(endpointPath, tokenPath string) (Endpoint, error) {
	data, err := os.ReadFile(endpointPath)
	if err != nil {
		return Endpoint{}, err
	}
	var endpoint Endpoint
	if err := json.Unmarshal(data, &endpoint); err != nil {
		return Endpoint{}, fmt.Errorf("ler endpoint da API local: %w", err)
	}
	if endpoint.Version != ProtocolVersion || endpoint.Address == "" || !sameTokenPath(endpoint.TokenFile, tokenPath) {
		return Endpoint{}, ErrInvalidEndpoint
	}
	endpoint.TokenFile = tokenPath
	host, _, err := net.SplitHostPort(endpoint.Address)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		return Endpoint{}, ErrInvalidEndpoint
	}
	return endpoint, nil
}

func sameTokenPath(written, expected string) bool {
	if filepath.Clean(written) == filepath.Clean(expected) {
		return true
	}
	if runtime.GOOS != "linux" || len(written) < 3 || written[1] != ':' {
		return false
	}
	drive := strings.ToLower(string(written[0]))
	mounted := "/mnt/" + drive + strings.ReplaceAll(written[2:], "\\", "/")
	return filepath.Clean(mounted) == filepath.Clean(expected)
}
