package caddy

import (
	"strings"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

func TestRenderWSLIsDeterministicAndSorted(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.Projects = []domain.Project{
		{Name: "zeta", Path: "/home/dev/zeta"},
		{Name: "alpha", Path: "/home/dev/alpha"},
	}
	first, err := RenderWSL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderWSL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("renderização não foi determinística")
	}
	if strings.Index(first, "@alpha") > strings.Index(first, "@zeta") {
		t.Fatal("rotas não foram ordenadas por nome")
	}
	if !strings.Contains(first, `root * "/home/dev/alpha/public"`) {
		t.Fatal("document root Laravel ausente")
	}
	if !strings.Contains(first, "handle_path /alpha/*") {
		t.Fatal("remoção de prefixo nativa ausente")
	}
	if !strings.Contains(first, "vars devlan_request_uri {http.request.uri}") {
		t.Fatal("captura da URI antes do FastCGI ausente")
	}
	if !strings.Contains(first, "env REQUEST_URI /alpha{vars.devlan_request_uri}") {
		t.Fatal("URI interna do FastCGI ausente")
	}
	if !strings.Contains(first, "env SCRIPT_NAME /alpha/index.php") {
		t.Fatal("subdiretório FastCGI ausente")
	}
	if !strings.Contains(first, "env HTTPS {http.request.header.X-DevLAN-HTTPS}") {
		t.Fatal("protocolo HTTPS original não é propagado ao FastCGI")
	}
	if !strings.Contains(first, "header_down Location ^/alpha/(.*)$ /$1") ||
		!strings.Contains(first, "header_down Location ^/(.*)$ /alpha/$1") {
		t.Fatal("normalização de redirects relativos ausente")
	}
	if !strings.Contains(first, "header_regexp Referer ^https?://[^/]+/alpha(?:/|$)") ||
		!strings.Contains(first, "handle @devlan_compat_0") {
		t.Fatal("compatibilidade com URLs absolutas do frontend ausente")
	}
	if !strings.Contains(first, "admin 127.0.0.1:2020") {
		t.Fatal("porta administrativa dedicada do WSL ausente")
	}
}

func TestRenderWindowsUsesDedicatedAdminAddress(t *testing.T) {
	result, err := RenderWindows(domain.NewConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "admin 127.0.0.1:2019") {
		t.Fatal("endereço administrativo do Windows ausente")
	}
}

func TestRenderWindowsWithInternalTLS(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.LANAddress = "192.168.10.77"
	cfg.TLSEnabled = true
	result, err := RenderWindows(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"default_sni 192.168.10.77", "http://:80", "https://192.168.10.77:443", "tls internal",
		"redir https://{http.request.host}{http.request.uri} 308",
		"header_up X-DevLAN-HTTPS on", "header_up -X-DevLAN-HTTPS",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("configuração TLS não contém %q:\n%s", expected, result)
		}
	}
}

func TestRenderWindowsRedirectsOnlyProjectsWithTLSPreference(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.TLSEnabled = true
	secure := true
	cfg.Projects = []domain.Project{
		{Name: "secure", Path: "/home/dev/secure", Secure: &secure},
		{Name: "plain", Path: "/home/dev/plain"},
	}
	result, err := RenderWindows(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "@devlan_secure_0 path /secure /secure/*") {
		t.Fatalf("redirect do projeto seguro ausente:\n%s", result)
	}
	if strings.Contains(result, "path /plain /plain/*") {
		t.Fatalf("projeto sem preferência TLS não deveria receber redirect:\n%s", result)
	}
}

func TestRenderRejectsFutureModeUntilImplemented(t *testing.T) {
	cfg := domain.NewConfig()
	mode := domain.ModeDev
	cfg.Projects = []domain.Project{{Name: "painel", Path: "/home/dev/painel", Mode: &mode}}
	if _, err := RenderWSL(cfg); err == nil || !strings.Contains(err.Error(), "não implementado") {
		t.Fatalf("modo futuro deveria ser rejeitado: %v", err)
	}
}
