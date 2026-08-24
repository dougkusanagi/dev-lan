package php

import (
	"strings"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

func TestPlansUseOndemandAndIdleTimeoutPerVersion(t *testing.T) {
	cfg := domain.NewConfig()
	if _, err := cfg.AddPHPVersion("8.3", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.AddPHPVersion("8.5", nil); err != nil {
		t.Fatal(err)
	}
	version := "8.3"
	isolated := true
	cfg.Projects = []domain.Project{{Name: "financeiro", Path: "/home/dev/financeiro", PHPVersion: &version, PHPIsolatedPool: &isolated}}
	plans, err := Plans(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 3 {
		t.Fatalf("esperava pools shared de duas versões e um isolado, obtido %d", len(plans))
	}
	contents := plans[0].Contents
	for _, expected := range []string{"pm = ondemand", "pm.max_children = 10", "pm.process_idle_timeout = 10s", "pm.max_requests = 500", "listen.owner = caddy"} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("configuração de pool não contém %q:\n%s", expected, contents)
		}
	}
	if !strings.Contains(plans[0].Contents, "/run/devlan/php/8.3/financeiro.sock") {
		t.Fatalf("socket isolado ausente:\n%s", plans[0].Contents)
	}
}

func TestInfoPageDoesNotExposeRuntimeEnvironment(t *testing.T) {
	cfg := domain.NewConfig()
	cfg.Projects = []domain.Project{{Name: "site", Path: "/home/dev/site"}}
	page, err := RenderInfoPage(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, "Informações sanitizadas") || strings.Contains(page, "$_SERVER") {
		t.Fatalf("página não parece sanitizada:\n%s", page)
	}
}
