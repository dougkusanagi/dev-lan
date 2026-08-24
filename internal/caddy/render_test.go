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
	if !strings.Contains(first, "request_header X-Forwarded-Prefix /alpha") {
		t.Fatal("prefixo encaminhado ausente")
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
