package platform

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestM8RealWindowsWSL is the opt-in end-to-end probe for a prepared Windows
// host. It deliberately skips in ordinary CI: the test needs Windows 11,
// WSL 2, systemd, mirrored networking and a Caddy installation. The optional
// CLI and URL checks are enabled by the environment variables documented in
// scripts/test-m8-real.ps1.
func TestM8RealWindowsWSL(t *testing.T) {
	if os.Getenv("DEVLAN_M8_REAL") != "1" {
		t.Skip("ative DEVLAN_M8_REAL=1 para o smoke real Windows+WSL")
	}
	if runtime.GOOS != "windows" {
		t.Skip("o smoke M8 real requer Windows")
	}
	wsl, err := exec.LookPath("wsl.exe")
	if err != nil {
		t.Fatal("wsl.exe não encontrado")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if output, err := runM8Command(ctx, wsl, "--version"); err != nil {
		t.Fatalf("wsl --version falhou: %v\n%s", err, output)
	}
	distribution := strings.TrimSpace(os.Getenv("DEVLAN_M8_WSL_DISTRIBUTION"))
	if distribution == "" {
		output, err := runM8Command(ctx, wsl, "--list", "--quiet")
		if err != nil {
			t.Fatalf("listar distribuições WSL falhou: %v\n%s", err, output)
		}
		for _, line := range strings.Split(strings.ReplaceAll(output, "\x00", ""), "\n") {
			if candidate := strings.TrimSpace(line); candidate != "" {
				distribution = candidate
				break
			}
		}
	}
	if distribution == "" {
		t.Fatal("nenhuma distribuição WSL disponível; defina DEVLAN_M8_WSL_DISTRIBUTION")
	}
	effectiveScript := `if [ "$(ps -p 1 -o comm= 2>/dev/null | tr -d ' ')" = systemd ]; then printf systemd=true; else printf systemd=false; fi`
	effective, err := runM8Command(ctx, wsl, "--distribution", distribution, "--exec", "/bin/sh", "-c", effectiveScript)
	if err != nil || strings.TrimSpace(effective) != "systemd=true" {
		t.Fatalf("systemd não está efetivo na distribuição %q: %v\n%s", distribution, err, effective)
	}

	devlan := strings.TrimSpace(os.Getenv("DEVLAN_M8_DEVLAN_BIN"))
	dataDir := strings.TrimSpace(os.Getenv("DEVLAN_M8_DATA_DIR"))
	if devlan != "" && dataDir != "" {
		reportOutput, err := runM8Command(ctx, devlan, "--data-dir", dataDir, "topology", "check", "--json")
		if err != nil {
			t.Fatalf("devlan topology check falhou: %v\n%s", err, reportOutput)
		}
		var report struct {
			Supported             bool           `json:"supported"`
			WSL2                  bool           `json:"wsl2"`
			MirroredNetworking    bool           `json:"mirroredNetworking"`
			Systemd               bool           `json:"systemd"`
			LoopbackBidirectional bool           `json:"loopbackBidirectional"`
			LANReachable          bool           `json:"lanReachable"`
			PortConflicts         []PortConflict `json:"portConflicts"`
		}
		if err := json.Unmarshal([]byte(reportOutput), &report); err != nil {
			t.Fatalf("topology check não retornou JSON válido: %v\n%s", err, reportOutput)
		}
		if !report.Supported || !report.WSL2 || !report.MirroredNetworking || !report.Systemd || !report.LoopbackBidirectional || !report.LANReachable || len(report.PortConflicts) > 0 {
			t.Fatalf("compatibilidade M8 não está saudável: %#v", report)
		}
		if output, err := runM8Command(ctx, devlan, "--data-dir", dataDir, "topology", "status", "--json"); err != nil {
			t.Fatalf("devlan topology status falhou: %v\n%s", err, output)
		}
	}

	if rawPort := strings.TrimSpace(os.Getenv("DEVLAN_M8_OCCUPIED_PORT")); rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			t.Fatalf("DEVLAN_M8_OCCUPIED_PORT inválida: %q", rawPort)
		}
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", rawPort))
		if err != nil {
			t.Fatalf("não foi possível reservar a porta de cenário %d: %v", port, err)
		}
		if devlan != "" && dataDir != "" {
			output, checkErr := runM8Command(ctx, devlan, "--data-dir", dataDir, "topology", "check", "--json")
			if checkErr != nil {
				listener.Close()
				t.Fatalf("topology check do cenário de porta falhou: %v\n%s", checkErr, output)
			}
			var report struct {
				PortConflicts []PortConflict `json:"portConflicts"`
			}
			if err := json.Unmarshal([]byte(output), &report); err != nil {
				listener.Close()
				t.Fatalf("topology check do cenário de porta não retornou JSON válido: %v", err)
			}
			found := false
			for _, conflict := range report.PortConflicts {
				if conflict.Port == port {
					found = true
					break
				}
			}
			if !found {
				listener.Close()
				t.Fatalf("porta ocupada %d não apareceu no diagnóstico: %#v", port, report.PortConflicts)
			}
		}
		listener.Close()
	}

	if os.Getenv("DEVLAN_M8_RUN_SHUTDOWN") == "1" {
		if output, err := runM8Command(ctx, wsl, "--shutdown"); err != nil {
			t.Fatalf("wsl --shutdown falhou: %v\n%s", err, output)
		}
		if devlan != "" && dataDir != "" {
			output, checkErr := runM8Command(ctx, devlan, "--data-dir", dataDir, "topology", "check", "--json")
			if checkErr != nil {
				t.Fatalf("topology check após wsl --shutdown falhou: %v\n%s", checkErr, output)
			}
			var report struct {
				Supported bool `json:"supported"`
			}
			if err := json.Unmarshal([]byte(output), &report); err != nil || !report.Supported {
				t.Fatalf("compatibilidade não voltou saudável após wsl --shutdown: %v\n%s", err, output)
			}
		}
	}

	for _, target := range splitM8URLs(os.Getenv("DEVLAN_M8_URLS")) {
		requestContext, requestCancel := context.WithTimeout(context.Background(), 5*time.Second)
		request, err := http.NewRequestWithContext(requestContext, http.MethodGet, target, nil)
		if err != nil {
			requestCancel()
			t.Fatalf("URL M8 inválida %q: %v", target, err)
		}
		client := &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // local Caddy CA is the subject of this smoke
			}},
		}
		response, err := client.Do(request)
		if err != nil {
			requestCancel()
			t.Fatalf("endpoint M8 %s não respondeu: %v", target, err)
		}
		response.Body.Close()
		requestCancel()
		if response.StatusCode >= http.StatusInternalServerError {
			t.Fatalf("endpoint M8 %s retornou HTTP %d", target, response.StatusCode)
		}
	}

}

func runM8Command(ctx context.Context, program string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, program, args...)
	output, err := command.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func splitM8URLs(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' ' })
}
