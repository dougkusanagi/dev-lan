package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWriteProjectTable(t *testing.T) {
	var output bytes.Buffer
	rows := []projectRow{{
		Name: "cj-crm", Mode: "php", Runtime: "8.5", Type: "laravel", Source: "global", SSL: "on",
		URL: "https://192.168.10.77/cj-crm", Path: "/home/silver/Sites/cj-crm",
	}}
	if err := writeProjectTable(&output, rows); err != nil {
		t.Fatal(err)
	}
	result := output.String()
	for _, expected := range []string{"PROJETO", "MODO", "RUNTIME", "TIPO", "ORIGEM", "SSL", "URL", "CAMINHO", "8.5", "laravel", "on", "cj-crm", "https://192.168.10.77/cj-crm", "/home/silver/Sites/cj-crm"} {
		if !strings.Contains(result, expected) {
			t.Fatalf("saída não contém %q:\n%s", expected, result)
		}
	}
}

func TestProjectRowJSON(t *testing.T) {
	row := projectRow{
		Name: "cj-crm", Mode: "php", Runtime: "8.5", Type: "laravel", Source: "global", SSL: "on",
		URL: "https://192.168.10.77/cj-crm", Path: "/home/silver/Sites/cj-crm",
	}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"name":"cj-crm"`, `"mode":"php"`, `"runtime":"8.5"`, `"type":"laravel"`, `"source":"global"`, `"ssl":"on"`, `"url":"https://192.168.10.77/cj-crm"`, `"path":"/home/silver/Sites/cj-crm"`} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("JSON não contém %q:\n%s", expected, string(data))
		}
	}
}

func TestPhase4CLIRuns(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	dir := t.TempDir()

	// Initialize
	if err := run([]string{"--data-dir", dir, "install", "--no-firewall"}); err != nil {
		t.Fatal(err)
	}

	// Link a project
	projDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "index.html"), []byte("<h1>test</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"--data-dir", dir, "link", "api", projDir}); err != nil {
		t.Fatal(err)
	}

	// Test route command with dynamic available port
	var freePort int
	if ln, err := net.Listen("tcp", "127.0.0.1:0"); err == nil {
		freePort = ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
	} else {
		freePort = 19482
	}
	if err := run([]string{"--data-dir", dir, "route"}); err != nil {
		t.Fatalf("route list failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "route", "api", "port", "--port", strconv.Itoa(freePort)}); err != nil {
		t.Fatalf("route set port failed: %v", err)
	}

	// Test expose command
	if err := run([]string{"--data-dir", dir, "expose", "api", "--duration", "1h"}); err != nil {
		t.Fatalf("expose failed: %v", err)
	}

	// Test unexpose command
	if err := run([]string{"--data-dir", dir, "unexpose", "api"}); err != nil {
		t.Fatalf("unexpose failed: %v", err)
	}

	// Test allowlist commands
	if err := run([]string{"--data-dir", dir, "allowlist"}); err != nil {
		t.Fatalf("allowlist list failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "allowlist", "set", "api", "192.168.1.0/24"}); err != nil {
		t.Fatalf("allowlist set failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "allowlist", "add", "api", "10.0.0.1"}); err != nil {
		t.Fatalf("allowlist add failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "allowlist", "remove", "api", "10.0.0.1"}); err != nil {
		t.Fatalf("allowlist remove failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "allowlist", "clear", "api"}); err != nil {
		t.Fatalf("allowlist clear failed: %v", err)
	}

	// Test auth commands
	if err := run([]string{"--data-dir", dir, "auth", "enable", "api", "alice", "p@ssword"}); err != nil {
		t.Fatalf("auth enable failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "auth", "disable", "api"}); err != nil {
		t.Fatalf("auth disable failed: %v", err)
	}

	// Test ca commands
	if err := run([]string{"--data-dir", dir, "ca", "info"}); err != nil {
		t.Fatalf("ca info failed: %v", err)
	}

	// Test dns commands
	if err := run([]string{"--data-dir", dir, "dns", "entries"}); err != nil {
		t.Fatalf("dns entries failed: %v", err)
	}

	// Test security commands
	if err := run([]string{"--data-dir", dir, "security", "posture"}); err != nil {
		t.Fatalf("security posture failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "security", "audit", "--lines", "10"}); err != nil {
		t.Fatalf("security audit failed: %v", err)
	}
}
