// Package api exposes the loopback web server and authenticated API
// shared by the browser GUI, Windows service, CLI and WSL clients.
package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Server) serveSPA(writer http.ResponseWriter, request *http.Request, distFS fs.FS) {
	if distFS == nil {
		ensureCSRFCookie(writer, request)
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("<!DOCTYPE html><html><body><h1>DevLAN</h1><p>Frontend não compilado.</p></body></html>"))
		return
	}

	reqPath := path.Clean(strings.TrimPrefix(request.URL.Path, "/"))
	if reqPath == "" || reqPath == "." {
		reqPath = "index.html"
	}

	// Try opening static assets. A missing asset must remain a 404; serving the
	// SPA document here makes browsers report misleading JS/CSS parse errors and
	// violates the API/static boundary.
	if strings.HasPrefix(reqPath, "assets/") || (reqPath != "index.html" && filepath.Ext(reqPath) != "" && filepath.Ext(reqPath) != ".html") {
		file, err := distFS.Open(reqPath)
		if err == nil {
			defer file.Close()
			stat, statErr := file.Stat()
			if statErr == nil && !stat.IsDir() {
				if strings.HasPrefix(reqPath, "assets/") {
					writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					writer.Header().Set("Cache-Control", "no-cache")
				}
				http.FileServer(http.FS(distFS)).ServeHTTP(writer, request)
				return
			}
		}
		http.NotFound(writer, request)
		return
	}

	// History fallback -> index.html
	indexFile, err := distFS.Open("index.html")
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer indexFile.Close()

	// Ensure CSRF cookie exists for SPA mutations
	ensureCSRFCookie(writer, request)

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	_, _ = io.Copy(writer, indexFile)
}

func ensureCSRFCookie(writer http.ResponseWriter, request *http.Request) {
	if _, err := request.Cookie(csrfCookieName); err != nil {
		tokenBytes := make([]byte, 16)
		_, _ = rand.Read(tokenBytes)
		token := hex.EncodeToString(tokenBytes)
		http.SetCookie(writer, &http.Cookie{
			Name:     csrfCookieName,
			Value:    token,
			Path:     "/",
			SameSite: http.SameSiteLaxMode,
			HttpOnly: false, // Accessible by JS client for CSRF header
		})
	}
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

func isValidHost(hostHeader string) bool {
	host, _, err := net.SplitHostPort(hostHeader)
	if err != nil {
		host = hostHeader
	}
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost" || host == "devlan.localhost"
}

func (s *Server) isValidOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if !strings.EqualFold(u.Scheme, "https") && !strings.EqualFold(u.Scheme, "http") {
		return false
	}

	// Origin is a full browser security principal: host alone is insufficient.
	// Cookies are not scoped by port, so accepting any loopback port would let a
	// malicious local web server set the double-submit CSRF cookie and mutate
	// this API. Only the actual direct UI origin and the fixed Caddy origin are
	// valid.
	if strings.EqualFold(u.Scheme, "https") && strings.EqualFold(u.Host, "devlan.localhost") {
		return true
	}
	if !strings.EqualFold(u.Scheme, "http") {
		return false
	}
	cfg, err := s.service.Config()
	if err != nil {
		return false
	}
	uiPort := cfg.UIPort
	if uiPort == 0 {
		uiPort = 3210
	}
	if u.Port() != strconv.Itoa(uiPort) {
		return false
	}
	host := strings.Trim(strings.ToLower(strings.TrimSpace(u.Hostname())), "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

func setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self' data:;")
}

func isMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	default:
		return false
	}
}

func isAuthorizedOrCSRF(request *http.Request, secret string) bool {
	// 1. Check Bearer token (CLI / WSL)
	if authorized(request, secret) {
		return true
	}

	// 2. Check CSRF header vs cookie (Browser)
	headerToken := strings.TrimSpace(request.Header.Get(csrfHeaderName))
	cookie, err := request.Cookie(csrfCookieName)
	if err == nil && headerToken != "" && cookie.Value != "" {
		if subtle.ConstantTimeCompare([]byte(headerToken), []byte(cookie.Value)) == 1 {
			return true
		}
	}

	return false
}

func authorized(request *http.Request, token string) bool {
	if token == "" {
		return false
	}
	value := strings.TrimSpace(request.Header.Get("Authorization"))
	if len(value) < len("Bearer ") || !strings.EqualFold(value[:len("Bearer ")], "Bearer ") {
		return false
	}
	provided := strings.TrimSpace(value[len("Bearer "):])
	if len(provided) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

// Handlers
