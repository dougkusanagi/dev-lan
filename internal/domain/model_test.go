package domain

import (
	"testing"
	"time"
)

func TestResolveModePriority(t *testing.T) {
	parkMode := ModeDev
	projectMode := ModeStatic
	cfg := NewConfig()
	cfg.DefaultMode = ModePHP
	cfg.Parks = []Park{{Path: "/home/silver/dev", Mode: &parkMode}}
	cfg.Projects = []Project{
		{Name: "financeiro", Path: "/home/silver/dev/financeiro", Mode: &projectMode},
		{Name: "painel", Path: "/home/silver/dev/painel"},
		{Name: "externo", Path: "/srv/external"},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}

	resolved, err := cfg.Resolve("financeiro")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Mode != ModeStatic || resolved.Source != SourceProject {
		t.Fatalf("projeto deveria vencer a herança: %#v", resolved)
	}

	resolved, err = cfg.Resolve("painel")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Mode != ModeDev || resolved.Source != SourcePark {
		t.Fatalf("park deveria vencer o padrão global: %#v", resolved)
	}

	resolved, err = cfg.Resolve("externo")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Mode != ModePHP || resolved.Source != SourceGlobal {
		t.Fatalf("padrão global inesperado: %#v", resolved)
	}
}

func TestNormalizeRejectsUnsafeRouteNamesAndRelativePaths(t *testing.T) {
	if _, err := NormalizeName("../financeiro"); err == nil {
		t.Fatal("nome com traversal deveria ser rejeitado")
	}
	if _, err := NormalizeName("Financeiro"); err != nil {
		t.Fatalf("nome capitalizado deveria ser normalizado: %v", err)
	}
	if _, err := NormalizePath("relative/project"); err == nil {
		t.Fatal("caminho relativo deveria ser rejeitado")
	}
}

func TestDirectChildParkOnlyMatchesOneLevel(t *testing.T) {
	cfg := NewConfig()
	parkMode := ModeDev
	cfg.Parks = []Park{{Path: "/home/dev", Mode: &parkMode}}
	cfg.Projects = []Project{
		{Name: "direct", Path: "/home/dev/direct"},
		{Name: "nested", Path: "/home/dev/nested/app"},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	direct, err := cfg.Resolve("direct")
	if err != nil {
		t.Fatal(err)
	}
	if direct.Source != SourcePark {
		t.Fatalf("filho direto deveria herdar park: %#v", direct)
	}
	nested, err := cfg.Resolve("nested")
	if err != nil {
		t.Fatal(err)
	}
	if nested.Source != SourceGlobal {
		t.Fatalf("filho aninhado não deveria herdar park: %#v", nested)
	}
}

func TestResolvedProjectURLUsesTLSState(t *testing.T) {
	project := ResolvedProject{Project: Project{Name: "financeiro"}, RoutePort: 8080}
	if got := project.URL("192.168.10.77", 80, 443, false); got != "http://192.168.10.77:8080/" {
		t.Fatalf("URL HTTP inesperada: %s", got)
	}
	if got := project.URL("192.168.10.77", 80, 443, true); got != "https://192.168.10.77:8080/" {
		t.Fatalf("URL HTTPS inesperada: %s", got)
	}
}

func TestLocalDevURLUsesProjectLocalhostOrigin(t *testing.T) {
	if got, want := LocalDevURL("cj-spec-sheet"), "https://cj-spec-sheet.localhost/"; got != want {
		t.Fatalf("LocalDevURL() = %q, want %q", got, want)
	}
}

func TestNormalizeNameRejectsReservedNames(t *testing.T) {
	for _, name := range []string{"devlan", "localhost", "api"} {
		if _, err := NormalizeName(name); err == nil {
			t.Fatalf("NormalizeName(%q) deveria rejeitar nome reservado", name)
		}
	}
	if normalized, err := NormalizeName("catalogo-app"); err != nil || normalized != "catalogo-app" {
		t.Fatalf("NormalizeName(catalogo-app) falhou: %v", err)
	}
}

func TestPHPVersionAndPoolResolution(t *testing.T) {
	cfg := NewConfig()
	if _, err := cfg.AddPHPVersion("8.3", []string{"xml", "mbstring"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddPHPVersion("8.5", []string{"curl"}); err != nil {
		t.Fatal(err)
	}
	isolated := true
	version := "8.3"
	cfg.Projects = []Project{{Name: "legacy", Path: "/home/dev/legacy", PHPVersion: &version, PHPIsolatedPool: &isolated}}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	project := cfg.Projects[0]
	if got := cfg.PHPSocket(project); got != "/run/devlan/php/8.3/legacy.sock" {
		t.Fatalf("socket isolado inesperado: %s", got)
	}
	if got := cfg.PHPPool(project); got.IdleTimeout != "10s" || got.MaxChildren != 10 || got.MaxRequests != 500 {
		t.Fatalf("pool PHP inesperado: %#v", got)
	}
	if got := cfg.EffectivePHPVersion(Project{Name: "other"}); got != "8.3" {
		t.Fatalf("versão global inesperada: %s", got)
	}
}

func TestPHPConfigRejectsUnsafeVersionAndPool(t *testing.T) {
	cfg := NewConfig()
	cfg.PHPDefaultVersion = "latest"
	if err := cfg.Normalize(); err == nil {
		t.Fatal("versão PHP arbitrária deveria ser rejeitada")
	}
	cfg = NewConfig()
	cfg.PHPFPMPool.IdleTimeout = "0s"
	if err := cfg.Normalize(); err == nil {
		t.Fatal("timeout PHP não positivo deveria ser rejeitado")
	}
}

func TestDevPortAllocationAndConflict(t *testing.T) {
	cfg := NewConfig()
	port1 := 9200
	port2 := 9200
	cfg.Projects = []Project{
		{Name: "app1", Path: "/home/dev/app1", DevPort: &port1},
		{Name: "app2", Path: "/home/dev/app2", DevPort: &port2},
	}
	if err := cfg.Normalize(); err == nil {
		t.Fatal("conflito de porta dev deveria ser rejeitado")
	}

	cfg2 := NewConfig()
	cfg2.Projects = []Project{
		{Name: "app1", Path: "/home/dev/app1"},
		{Name: "app2", Path: "/home/dev/app2"},
	}
	if err := cfg2.Normalize(); err != nil {
		t.Fatal(err)
	}
	p1Port := cfg2.DevPort(cfg2.Projects[0])
	p2Port := cfg2.DevPort(cfg2.Projects[1])
	if p1Port == p2Port {
		t.Fatalf("portas dev automáticas deveriam ser distintas: %d vs %d", p1Port, p2Port)
	}
	if p1Port != 9100 || p2Port != 9101 {
		t.Fatalf("portas dev automáticas inesperadas: %d, %d", p1Port, p2Port)
	}
}

func TestStaticDocumentRootAndSPAFallback(t *testing.T) {
	cfg := NewConfig()
	dist := "dist"
	spaFalse := false
	cfg.Projects = []Project{
		{Name: "spa", Path: "/home/dev/spa", StaticDir: &dist},
		{Name: "plain", Path: "/home/dev/plain", SPAFallback: &spaFalse},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	spaProj, _ := cfg.Project("spa")
	plainProj, _ := cfg.Project("plain")
	if got := cfg.StaticDocumentRoot(spaProj); got != "/home/dev/spa/dist" {
		t.Fatalf("document root estático inesperado: %s", got)
	}
	if got := cfg.StaticDocumentRoot(plainProj); got != "/home/dev/plain" {
		t.Fatalf("document root estático inesperado: %s", got)
	}
	if !cfg.SPAFallback(spaProj) {
		t.Fatal("SPA fallback padrão deveria ser true")
	}
	if cfg.SPAFallback(plainProj) {
		t.Fatal("SPA fallback configurado deveria ser false")
	}
}

func TestRoutePortResolutionAndURL(t *testing.T) {
	cfg := NewConfig()
	customPort := 8085

	cfg.Projects = []Project{
		{Name: "automatic-app", Path: "/home/dev/automatic-app"},
		{Name: "custom-app", Path: "/home/dev/custom-app", RoutePort: &customPort},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}

	p1, err := cfg.Resolve("automatic-app")
	if err != nil {
		t.Fatal(err)
	}
	if p1.RoutePort != 8080 {
		t.Fatalf("porta LAN automática inesperada: %d", p1.RoutePort)
	}
	if got := p1.URL("192.168.1.50", 80, 443, false); got != "http://192.168.1.50:8080/" {
		t.Fatalf("URL LAN automática inesperada: %s", got)
	}

	p2, err := cfg.Resolve("custom-app")
	if err != nil {
		t.Fatal(err)
	}
	if p2.RoutePort != 8085 {
		t.Fatalf("porta LAN customizada inesperada: %d", p2.RoutePort)
	}
	if got := p2.URL("192.168.1.50", 80, 443, false); got != "http://192.168.1.50:8085/" {
		t.Fatalf("URL LAN customizada inesperada: %s", got)
	}
}

func TestAllowlistNormalizationAndEffective(t *testing.T) {
	cfg := NewConfig()
	cfg.Allowlist = []string{"192.168.1.0/24", "10.0.0.1"}
	cfg.Projects = []Project{
		{Name: "app1", Path: "/home/dev/app1"},
		{Name: "app2", Path: "/home/dev/app2", Allowlist: []string{"172.16.0.0/16"}},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if cfg.Allowlist[0] != "10.0.0.1/32" && cfg.Allowlist[1] != "10.0.0.1/32" {
		t.Fatalf("normalização de IP único falhou: %#v", cfg.Allowlist)
	}
	app1, _ := cfg.Project("app1")
	app2, _ := cfg.Project("app2")
	if len(cfg.EffectiveAllowlist(app1)) != 2 {
		t.Fatalf("app1 deveria herdar allowlist global: %#v", cfg.EffectiveAllowlist(app1))
	}
	if got := cfg.EffectiveAllowlist(app2); len(got) != 1 || got[0] != "172.16.0.0/16" {
		t.Fatalf("app2 deveria ter sua própria allowlist: %#v", got)
	}
}

func TestExposureExpiration(t *testing.T) {
	cfg := NewConfig()
	future := "2030-01-01T00:00:00Z"
	past := "2020-01-01T00:00:00Z"
	cfg.Projects = []Project{
		{Name: "future-app", Path: "/home/dev/future-app", ExposedUntil: &future},
		{Name: "past-app", Path: "/home/dev/past-app", ExposedUntil: &past},
		{Name: "permanent-app", Path: "/home/dev/permanent-app"},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 18, 0, 0, 0, time.UTC)
	futureApp, _ := cfg.Project("future-app")
	pastApp, _ := cfg.Project("past-app")
	permApp, _ := cfg.Project("permanent-app")

	if cfg.IsExposureExpired(futureApp, now) {
		t.Fatal("future-app não deveria estar expirado")
	}
	if !cfg.IsExposureExpired(pastApp, now) {
		t.Fatal("past-app deveria estar expirado")
	}
	if cfg.IsExposureExpired(permApp, now) {
		t.Fatal("permanent-app não deveria estar expirado")
	}
}
