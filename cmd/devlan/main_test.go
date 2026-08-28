package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func captureCLIOutput(t *testing.T, action func()) string {
	t.Helper()
	previous := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	action()
	_ = writer.Close()
	os.Stdout = previous
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCLIGlobalDispatchAndValidation(t *testing.T) {
	versionOutput := captureCLIOutput(t, func() {
		if err := run([]string{"version"}); err != nil {
			t.Fatalf("version retornou erro: %v", err)
		}
	})
	if versionOutput != version+"\n" {
		t.Fatalf("saída de version = %q, esperado %q", versionOutput, version+"\n")
	}

	if _, _, err := parseGlobalArgs([]string{"--data-dir"}); err == nil || err.Error() != "--data-dir exige um caminho" {
		t.Fatalf("erro de --data-dir mudou: %v", err)
	}
	if err := run([]string{"config"}); err == nil || !strings.Contains(err.Error(), "uso: devlan config export") {
		t.Fatalf("erro de validação de config mudou: %v", err)
	}
}

func TestCLIEntrypointMapsCommandErrorToExitCode(t *testing.T) {
	if os.Getenv("DEVLAN_CLI_EXIT_CHILD") == "1" {
		os.Args = []string{"devlan", "comando-inexistente"}
		main()
		return
	}
	command := exec.Command(os.Args[0], "-test.run=TestCLIEntrypointMapsCommandErrorToExitCode", "-test.v=false")
	command.Env = append(os.Environ(), "DEVLAN_CLI_EXIT_CHILD=1")
	err := command.Run()
	if err == nil {
		t.Fatal("entrypoint de comando inválido deveria retornar código diferente de zero")
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("código de saída inesperado: %v", err)
	}
}

func TestTopologyMigrationGetsColdWSLStartupBudget(t *testing.T) {
	if got := cliCommandTimeout("topology", []string{"migrate", "--yes"}); got != 3*time.Minute {
		t.Fatalf("timeout da migração = %s", got)
	}
	if got := cliCommandTimeout("topology", []string{"check"}); got != 45*time.Second {
		t.Fatalf("timeout do check = %s", got)
	}
}

func TestWriteProjectTable(t *testing.T) {
	var output bytes.Buffer
	rows := []projectRow{{
		Name: "cj-crm", Mode: "php", Runtime: "8.5", Type: "laravel", Source: "global", SSL: "on",
		URL: "https://192.168.10.77:8080/", LocalURL: "https://cj-crm.localhost/", LANURL: "https://192.168.10.77:8080/", Path: "/home/silver/Sites/cj-crm",
	}}
	if err := writeProjectTable(&output, rows); err != nil {
		t.Fatal(err)
	}
	result := output.String()
	for _, expected := range []string{"PROJETO", "MODO", "RUNTIME", "TIPO", "SSL", "URL LOCAL", "URL LAN", "CAMINHO", "8.5", "laravel", "on", "cj-crm", "https://cj-crm.localhost/", "https://192.168.10.77:8080/", "/home/silver/Sites/cj-crm"} {
		if !strings.Contains(result, expected) {
			t.Fatalf("saída não contém %q:\n%s", expected, result)
		}
	}
}

func TestProjectRowJSON(t *testing.T) {
	row := projectRow{
		Name: "cj-crm", Mode: "php", Runtime: "8.5", Type: "laravel", Source: "global", SSL: "on",
		URL: "https://192.168.10.77:8080/", LocalURL: "https://cj-crm.localhost/", LANURL: "https://192.168.10.77:8080/", Path: "/home/silver/Sites/cj-crm",
	}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"name":"cj-crm"`, `"mode":"php"`, `"runtime":"8.5"`, `"type":"laravel"`, `"source":"global"`, `"ssl":"on"`, `"url":"https://192.168.10.77:8080/"`, `"local_url":"https://cj-crm.localhost/"`, `"lan_url":"https://192.168.10.77:8080/"`, `"path":"/home/silver/Sites/cj-crm"`} {
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
	if err := run([]string{"--data-dir", dir, "link", "web-api", projDir}); err != nil {
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
	if err := run([]string{"--data-dir", dir, "route", "web-api", "--port", strconv.Itoa(freePort)}); err != nil {
		t.Fatalf("route set port failed: %v", err)
	}

	// Test expose command
	if err := run([]string{"--data-dir", dir, "expose", "web-api", "--duration", "1h"}); err != nil {
		t.Fatalf("expose failed: %v", err)
	}

	// Test unexpose command
	if err := run([]string{"--data-dir", dir, "unexpose", "web-api"}); err != nil {
		t.Fatalf("unexpose failed: %v", err)
	}

	// Test allowlist commands
	if err := run([]string{"--data-dir", dir, "allowlist"}); err != nil {
		t.Fatalf("allowlist list failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "allowlist", "set", "web-api", "192.168.1.0/24"}); err != nil {
		t.Fatalf("allowlist set failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "allowlist", "add", "web-api", "10.0.0.1"}); err != nil {
		t.Fatalf("allowlist add failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "allowlist", "remove", "web-api", "10.0.0.1"}); err != nil {
		t.Fatalf("allowlist remove failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "allowlist", "clear", "web-api"}); err != nil {
		t.Fatalf("allowlist clear failed: %v", err)
	}

	// Test auth commands
	if err := run([]string{"--data-dir", dir, "auth", "enable", "web-api", "alice", "p@ssword"}); err != nil {
		t.Fatalf("auth enable failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "auth", "disable", "web-api"}); err != nil {
		t.Fatalf("auth disable failed: %v", err)
	}

	// Test ca commands
	if err := run([]string{"--data-dir", dir, "ca", "info"}); err != nil {
		t.Fatalf("ca info failed: %v", err)
	}

	// Test security commands
	if err := run([]string{"--data-dir", dir, "security", "posture"}); err != nil {
		t.Fatalf("security posture failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "security", "audit", "--lines", "10"}); err != nil {
		t.Fatalf("security audit failed: %v", err)
	}

	// Test desktop commands
	if err := run([]string{"--data-dir", dir, "desktop", "status"}); err != nil {
		t.Fatalf("desktop status failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "desktop", "install"}); err != nil {
		t.Fatalf("desktop install failed: %v", err)
	}
	if err := run([]string{"--data-dir", dir, "desktop", "uninstall"}); err != nil {
		t.Fatalf("desktop uninstall failed: %v", err)
	}
}
