package domain

import "testing"

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
	project := ResolvedProject{Project: Project{Name: "financeiro"}}
	if got := project.URL("192.168.10.77", 80, 443, false); got != "http://192.168.10.77/financeiro" {
		t.Fatalf("URL HTTP inesperada: %s", got)
	}
	if got := project.URL("192.168.10.77", 80, 443, true); got != "https://192.168.10.77/financeiro" {
		t.Fatalf("URL HTTPS inesperada: %s", got)
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
