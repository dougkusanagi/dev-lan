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
	if !strings.Contains(first, "@devlan_port_alpha header X-DevLAN-Port 8080") {
		t.Fatal("rota LAN por porta ausente")
	}
	if !strings.Contains(first, "env HTTPS {http.request.header.X-DevLAN-HTTPS}") {
		t.Fatal("protocolo HTTPS original não é propagado ao FastCGI")
	}
	if strings.Contains(first, "handle_path") || strings.Contains(first, "Referer") {
		t.Fatal("a rota não deve depender de subpath ou Referer")
	}
	if !strings.Contains(first, "admin 127.0.0.1:2020") {
		t.Fatal("porta administrativa dedicada do WSL ausente")
	}
}

func TestRenderWSLUnifiedOwnsEveryM8Edge(t *testing.T) {
	staticMode := domain.ModeStatic
	devMode := domain.ModeDev
	dist := "dist"
	laravel := domain.PHPPresetLaravel
	devPort := 9150
	cfg := domain.NewConfig()
	cfg.UIPort = 3210
	cfg.LANAddress = "192.168.10.77"
	cfg.TLSEnabled = true
	cfg.Projects = []domain.Project{
		{Name: "php-app", Path: "/home/dev/php-app", PHPPreset: &laravel},
		{Name: "static-app", Path: "/home/dev/static-app", Mode: &staticMode, StaticDir: &dist},
		{Name: "vite-app", Path: "/home/dev/vite-app", Mode: &devMode, DevPort: &devPort},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	result, err := RenderWSLUnified(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"admin 127.0.0.1:2020",
		"auto_https disable_redirects",
		"https://devlan.localhost",
		"reverse_proxy 127.0.0.1:3210",
		"https://php-app.localhost",
		"@devlan_local_vite_php-app path /@* /resources/* /node_modules/* /__laravel_vite_plugin__/* /src/*",
		"reverse_proxy 127.0.0.1:19100",
		"root * \"/home/dev/php-app/public\"",
		"https://192.168.10.77:8080",
		"root * \"/home/dev/static-app/dist\"",
		"reverse_proxy 127.0.0.1:9150",
		"bind 0.0.0.0",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("Caddyfile unificado não contém %q:\n%s", expected, result)
		}
	}
	for _, forbidden := range []string{"X-DevLAN-", "127.0.0.1:2019", "8181"} {
		if strings.Contains(result, forbidden) {
			t.Fatalf("Caddyfile unificado contém protocolo legado %q:\n%s", forbidden, result)
		}
	}
	if strings.Contains(result, "@devlan_local_vite_vite-app") {
		t.Fatal("Vite não deve ser publicado na rota LAN do projeto")
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
	secure := true
	cfg.Projects = []domain.Project{{Name: "secure", Path: "/home/dev/secure", Secure: &secure}}
	result, err := RenderWindows(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"default_sni 192.168.10.77", "https://192.168.10.77:8080", "tls internal",
		"header_up X-DevLAN-Port 8080", "header_up X-DevLAN-HTTPS on",
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
	if !strings.Contains(result, "https://localhost:8081 {") {
		t.Fatalf("listener seguro dedicado ausente:\n%s", result)
	}
	if !strings.Contains(result, ":8081 {") {
		t.Fatalf("listener HTTP dedicado ausente:\n%s", result)
	}
	if strings.Contains(result, "Referer") || strings.Contains(result, "path /secure") {
		t.Fatalf("TLS não deve criar compatibilidade por subpath:\n%s", result)
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
	for _, expected := range []string{":8080 {"} {
		if !strings.Contains(result, expected) {
			t.Fatalf("listener HTTP não contém %q:\n%s", expected, result)
		}
	}
	if strings.Contains(result, "Referer") || strings.Contains(result, "handle_path") || strings.Contains(result, "Clear-Site-Data") {
		t.Fatalf("projeto HTTP não deve criar branches de subpath/TLS legado:\n%s", result)
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
	if !strings.Contains(result, `@devlan_port_frontend header X-DevLAN-Port 8080`) {
		t.Fatalf("rota estática por porta ausente:\n%s", result)
	}
	if !strings.Contains(result, `root * "/home/dev/frontend/dist"`) {
		t.Fatalf("document root estático ausente:\n%s", result)
	}
	if !strings.Contains(result, "try_files {path} {path}/ /index.html") {
		t.Fatalf("SPA fallback ausente:\n%s", result)
	}

	// Check dev route
	if !strings.Contains(result, `@devlan_port_vite-app header X-DevLAN-Port 8081`) {
		t.Fatalf("rota dev por porta ausente:\n%s", result)
	}
	if !strings.Contains(result, `reverse_proxy 127.0.0.1:9300`) {
		t.Fatalf("proxy reverso dev ausente:\n%s", result)
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
		"@devlan_local_spec-sheet {\n        header_regexp Host ^spec-sheet\\.localhost(?::\\d+)?$\n        header X-DevLAN-Local on\n    }",
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

func TestRenderProjectsAlwaysUseDedicatedLANPorts(t *testing.T) {
	cfg := domain.NewConfig()
	devMode := domain.ModeDev
	port := 8089
	devPort := 9350

	cfg.Projects = []domain.Project{
		{Name: "port-proj", Path: "/home/dev/port-proj", RoutePort: &port, Mode: &devMode, DevPort: &devPort},
		{Name: "automatic-proj", Path: "/home/dev/automatic-proj"},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}

	winResult, err := RenderWindows(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(winResult, ":8089 {") || !strings.Contains(winResult, "header_up X-DevLAN-Port 8089") || !strings.Contains(winResult, ":8080 {") {
		t.Fatalf("listeners Windows por porta ausentes:\n%s", winResult)
	}

	wslResult, err := RenderWSL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wslResult, "@devlan_port_port-proj header X-DevLAN-Port 8089") || !strings.Contains(wslResult, "reverse_proxy 127.0.0.1:9350") {
		t.Fatalf("handler WSL por porta ausente:\n%s", wslResult)
	}
	if !strings.Contains(wslResult, "@devlan_port_automatic-proj header X-DevLAN-Port 8080") {
		t.Fatalf("handler WSL automático ausente:\n%s", wslResult)
	}
	if strings.Contains(wslResult, "Referer") || strings.Contains(wslResult, "handle_path") || strings.Contains(wslResult, "devlan_host_") {
		t.Fatalf("configuração WSL contém branch de roteamento removido:\n%s", wslResult)
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

func TestRenderWindowsAndWSLHeaderSecurityAndLoopbackRestriction(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.Projects = []domain.Project{
		{Name: "myapp", Path: "/home/dev/myapp"},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	winResult, err := RenderWindows(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"header_up -X-DevLAN-Port",
		"header_up -X-DevLAN-Local",
		"header_up -X-DevLAN-HTTPS",
		"header_up -X-Forwarded-For",
		"header_up X-DevLAN-Port 8080",
		"header_up X-DevLAN-Project myapp",
		"header_up X-Forwarded-Port 8080",
		"respond \"Acesso local permitido somente via loopback\" 403",
	} {
		if !strings.Contains(winResult, expected) {
			t.Fatalf("Caddyfile Windows não contém segurança esperada %q:\n%s", expected, winResult)
		}
	}

	wslResult, err := RenderWSL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wslResult, "bind 127.0.0.1") {
		t.Fatalf("Caddyfile WSL deveria conter bind 127.0.0.1:\n%s", wslResult)
	}
	if !strings.Contains(wslResult, "header X-DevLAN-Local on") {
		t.Fatalf("rota .localhost no WSL deve exigir a identidade local de confiança:\n%s", wslResult)
	}
}

func TestRenderWSLDoesNotTreatForgedLocalHostAsLocalOrigin(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.Projects = []domain.Project{{Name: "myapp", Path: "/home/dev/myapp"}}
	result, err := RenderWSL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	localMatcher := "@devlan_local_myapp {\n        header_regexp Host ^myapp\\.localhost(?::\\d+)?$\n        header X-DevLAN-Local on\n    }"
	if !strings.Contains(result, localMatcher) {
		t.Fatalf("Host .localhost sem X-DevLAN-Local não pode selecionar a rota local:\n%s", result)
	}
}

func TestRenderWindowsRedirectsLocalHTTPOnlyFromLoopback(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.Projects = []domain.Project{{Name: "myapp", Path: "/home/dev/myapp"}}
	result, err := RenderWindows(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"http://devlan.localhost http://myapp.localhost {",
		"@devlan_local_http_loopback remote_ip 127.0.0.1 ::1",
		"redir @devlan_local_http_loopback https://{http.request.host}{uri} permanent",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("redirect HTTP local ausente ou exposto indevidamente (%q):\n%s", expected, result)
		}
	}
}

func TestRenderWindowsRoutesDevLANLocalhostToUIPort(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.UIPort = 3210
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	result, err := RenderWindows(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"https://devlan.localhost {",
		"bind 0.0.0.0",
		"@devlan_admin_edge {",
		"host devlan.localhost",
		"remote_ip 127.0.0.1 ::1",
		"reverse_proxy 127.0.0.1:3210",
		"respond \"Acesso local permitido somente via loopback\" 403",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("Caddyfile Windows não contém rota de devlan.localhost esperada %q:\n%s", expected, result)
		}
	}
}
