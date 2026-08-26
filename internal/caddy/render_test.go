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
		"redir https://{http.request.host}{http.request.uri} 307",
		"header_up X-DevLAN-HTTPS on", "header_up -X-DevLAN-HTTPS",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("configuração TLS não contém %q:\n%s", expected, result)
		}
	}
}

func TestRenderWindowsSeparatesLocalHMRFromLANPreview(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.LANAddress = "192.168.10.77"
	cfg.TLSEnabled = true
	preset := domain.PHPPresetLaravel
	devPort := 9107
	cfg.Projects = []domain.Project{
		{Name: "spec-sheet", Path: "/home/dev/spec-sheet", PHPPreset: &preset, DevPort: &devPort},
	}

	result, err := RenderWindows(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"https://spec-sheet.localhost {",
		"remote_ip 127.0.0.1 ::1",
		"@devlan_local_vite_spec-sheet path /@* /resources/* /node_modules/* /__laravel_vite_plugin__/* /src/*",
		"reverse_proxy 127.0.0.1:19107",
		"header_up X-DevLAN-Project spec-sheet",
		"header_up X-DevLAN-Local on",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("proxy Vite do Laravel não contém %q:\n%s", expected, result)
		}
	}
	if strings.Contains(result, "@devlan_vite_spec-sheet") {
		t.Fatalf("Vite não deveria ser publicado no prefixo LAN:\n%s", result)
	}
}

func TestRenderWindowsGroupsLocalTLSHostsWhenLANTLSIsDisabled(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.TLSEnabled = false
	cfg.Projects = []domain.Project{
		{Name: "alpha", Path: "/home/dev/alpha"},
		{Name: "beta", Path: "/home/dev/beta"},
	}
	result, err := RenderWindows(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "https://alpha.localhost https://beta.localhost {") {
		t.Fatalf("hosts locais não foram agrupados no listener seguro:\n%s", result)
	}
	if strings.Count(result, "tls internal") != 1 {
		t.Fatalf("deveria existir um único bloco TLS local:\n%s", result)
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
	if !strings.Contains(result, "@devlan_secure_referer_0 header_regexp Referer ^https://[^/]+/secure(?:/|$)") {
		t.Fatalf("assets absolutos do projeto seguro não estão protegidos:\n%s", result)
	}
	if strings.Contains(result, "path /plain /plain/*") {
		t.Fatalf("projeto sem preferência TLS não deveria receber redirect:\n%s", result)
	}
}

func TestRenderWindowsClearsSecureBrowserStateBeforeDowngradingAProject(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.LANAddress = "192.168.10.77"
	cfg.TLSEnabled = true
	secure := false
	cfg.Projects = []domain.Project{
		{Name: "plain", Path: "/home/dev/plain", Secure: &secure},
	}
	result, err := RenderWindows(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"@devlan_unsecure_0 path /plain /plain/*",
		`header Clear-Site-Data "\"cache\", \"cookies\""`,
		"redir http://{http.request.host}{http.request.uri} 302",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("transição para HTTP não contém %q:\n%s", expected, result)
		}
	}
	if strings.Contains(result, "redir @devlan_secure_") {
		t.Fatalf("projeto sem TLS não deveria redirecionar HTTP para HTTPS:\n%s", result)
	}
	if strings.Contains(result, "handle {\n        header Clear-Site-Data") {
		t.Fatalf("limpeza não deve atingir recursos HTTPS fora do projeto:\n%s", result)
	}
}

func TestRenderWSLStaticAndDevModes(t *testing.T) {
	cfg := domain.NewConfig()
	staticMode := domain.ModeStatic
	devMode := domain.ModeDev
	dist := "dist"
	devPort := 9300
	cfg.Projects = []domain.Project{
		{Name: "frontend", Path: "/home/dev/frontend", Mode: &staticMode, StaticDir: &dist},
		{Name: "vite-app", Path: "/home/dev/vite-app", Mode: &devMode, DevPort: &devPort},
	}
	result, err := RenderWSL(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Check static route
	if !strings.Contains(result, `handle_path /frontend/*`) {
		t.Fatalf("rota estática ausente:\n%s", result)
	}
	if !strings.Contains(result, `root * "/home/dev/frontend/dist"`) {
		t.Fatalf("document root estático ausente:\n%s", result)
	}
	if !strings.Contains(result, "try_files {path} {path}/ /index.html") {
		t.Fatalf("SPA fallback ausente:\n%s", result)
	}

	// Check dev route
	if !strings.Contains(result, `handle_path /vite-app/*`) {
		t.Fatalf("rota dev ausente:\n%s", result)
	}
	if !strings.Contains(result, `reverse_proxy 127.0.0.1:9300`) {
		t.Fatalf("proxy reverso dev ausente:\n%s", result)
	}
	if !strings.Contains(result, `header_up X-DevLAN-Prefix /vite-app`) {
		t.Fatalf("header de prefixo ausente:\n%s", result)
	}
	if !strings.Contains(result, `header_up Upgrade {http.request.header.Upgrade}`) {
		t.Fatalf("suporte a websocket/HMR ausente:\n%s", result)
	}
}

func TestRenderWSLAddsIsolatedLocalHostRoute(t *testing.T) {
	cfg := domain.NewConfig()
	preset := domain.PHPPresetLaravel
	cfg.Projects = []domain.Project{{Name: "spec-sheet", Path: "/home/dev/spec-sheet", PHPPreset: &preset}}
	result, err := RenderWSL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"@devlan_local_spec-sheet header_regexp Host ^spec-sheet\\.localhost(?::\\d+)?$",
		"root * \"/home/dev/spec-sheet/public\"",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("rota local WSL não contém %q:\n%s", expected, result)
		}
	}
}

func TestRenderWSLUsesOneSocketPerPHPVersionAndProjectPool(t *testing.T) {
	cfg := domain.NewConfig()
	if _, err := cfg.AddPHPVersion("8.3", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddPHPVersion("8.5", nil); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetDefaultPHPVersion("8.5"); err != nil {
		t.Fatal(err)
	}
	version := "8.3"
	isolated := true
	cfg.Projects = []domain.Project{
		{Name: "old", Path: "/home/dev/old", PHPVersion: &version, PHPIsolatedPool: &isolated},
		{Name: "new", Path: "/home/dev/new"},
	}
	result, err := RenderWSL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"php_fastcgi unix//run/devlan/php/8.3/old.sock",
		"php_fastcgi unix//run/devlan/php/8.5/shared.sock",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("socket PHP ausente %q:\n%s", expected, result)
		}
	}
}

func TestRenderWSLUsesGenericDocumentRoot(t *testing.T) {
	cfg := domain.NewConfig()
	preset := domain.PHPPresetGeneric
	cfg.Projects = []domain.Project{{Name: "site", Path: "/home/dev/site", PHPPreset: &preset}}
	result, err := RenderWSL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `root * "/home/dev/site"`) {
		t.Fatalf("document root genérico inesperado:\n%s", result)
	}
}

func TestRenderPhase4RouteModes(t *testing.T) {
	cfg := domain.NewConfig()
	portMode := domain.RouteModePort
	hostMode := domain.RouteModeHost
	devMode := domain.ModeDev
	port := 8089
	devPort := 9350
	host := "meu-app.lan"

	cfg.Projects = []domain.Project{
		{Name: "port-proj", Path: "/home/dev/port-proj", RouteMode: &portMode, RoutePort: &port, Mode: &devMode, DevPort: &devPort},
		{Name: "host-proj", Path: "/home/dev/host-proj", RouteMode: &hostMode, RouteHost: &host},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}

	winResult, err := RenderWindows(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(winResult, ":8089 {") || !strings.Contains(winResult, "header_up X-DevLAN-Port 8089") {
		t.Fatalf("listener Windows para modo port ausente:\n%s", winResult)
	}

	wslResult, err := RenderWSL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Check port mode handler in WSL
	if !strings.Contains(wslResult, "@devlan_port_port-proj header X-DevLAN-Port 8089") || !strings.Contains(wslResult, "reverse_proxy 127.0.0.1:9350") {
		t.Fatalf("handler WSL para modo port ausente:\n%s", wslResult)
	}
	// Check host mode in WSL
	if !strings.Contains(wslResult, "@devlan_host_host-proj header_regexp Host ^meu-app\\.lan") {
		t.Fatalf("matcher host mode em WSL ausente:\n%s", wslResult)
	}
}

func TestRenderAllowlistAndBasicAuth(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.Allowlist = []string{"192.168.1.0/24"}
	cfg.AuthUsers = []domain.AuthUser{{Username: "admin", PasswordHash: "$2a$14$test"}}
	cfg.Projects = []domain.Project{
		{Name: "secure-app", Path: "/home/dev/secure-app"},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	wslResult, err := RenderWSL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wslResult, "@devlan_denied_secure-app not remote_ip 192.168.1.0/24") {
		t.Fatalf("allowlist matcher ausente:\n%s", wslResult)
	}
	if !strings.Contains(wslResult, "basicauth {") || !strings.Contains(wslResult, "admin $2a$14$test") {
		t.Fatalf("basicauth block ausente:\n%s", wslResult)
	}
}

func TestRenderExpiredProject(t *testing.T) {
	cfg := domain.NewConfig()
	past := "2020-01-01T00:00:00Z"
	cfg.Projects = []domain.Project{
		{Name: "expired-app", Path: "/home/dev/expired-app", ExposedUntil: &past},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	wslResult, err := RenderWSL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wslResult, `respond "Acesso expirado" 403`) {
		t.Fatalf("resposta de expiração ausente no Caddyfile:\n%s", wslResult)
	}
}
