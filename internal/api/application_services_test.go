package api

import (
	"context"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

type explicitProjectPort struct {
	linkedName string
	linkedPath string
}

func (p *explicitProjectPort) Link(_ context.Context, name, path string) (domain.Project, application.ApplyResult, error) {
	p.linkedName = name
	p.linkedPath = path
	return domain.Project{Name: name, Path: path}, application.ApplyResult{Revision: 12}, nil
}

func (p *explicitProjectPort) Unlink(context.Context, string) (domain.Project, application.ApplyResult, error) {
	return domain.Project{}, application.ApplyResult{}, nil
}

func (p *explicitProjectPort) Park(context.Context, string) (domain.Park, application.ApplyResult, error) {
	return domain.Park{}, application.ApplyResult{}, nil
}

func (p *explicitProjectPort) Unpark(context.Context, string) (domain.Park, application.ApplyResult, error) {
	return domain.Park{}, application.ApplyResult{}, nil
}

func (p *explicitProjectPort) IgnoreProject(context.Context, string) (application.ApplyResult, error) {
	return application.ApplyResult{}, nil
}

func (p *explicitProjectPort) UnignoreProject(context.Context, string) (application.ApplyResult, error) {
	return application.ApplyResult{}, nil
}

func (p *explicitProjectPort) SetDefaultMode(context.Context, domain.Mode) (application.ApplyResult, error) {
	return application.ApplyResult{}, nil
}

func (p *explicitProjectPort) SetProjectMode(context.Context, string, *domain.Mode) (application.ApplyResult, error) {
	return application.ApplyResult{}, nil
}

type explicitSettingsPort struct{}

func (explicitSettingsPort) SaveGlobalSettings(context.Context, application.GlobalSettings) (application.ApplyResult, error) {
	return application.ApplyResult{Status: "applied"}, nil
}

type explicitQueryPort struct {
	cfg domain.Config
}

func (p explicitQueryPort) Config() (domain.Config, error) { return p.cfg, nil }

func (p explicitQueryPort) EffectiveConfig(_ context.Context, cfg domain.Config) (domain.Config, error) {
	return cfg, nil
}

func (p explicitQueryPort) Revision() uint64 { return p.cfg.Revision }

func TestServerUsesExplicitApplicationServices(t *testing.T) {
	projects := &explicitProjectPort{}
	commands := application.NewCommands(projects, explicitSettingsPort{})
	cfg := domain.NewConfig()
	cfg.Revision = 19
	queries := application.NewQueries(explicitQueryPort{cfg: cfg})
	server := NewWithApplication(nil, commands, queries)

	if server.commands != commands || server.queries != queries {
		t.Fatal("o servidor não reteve os serviços de aplicação fornecidos")
	}
	project, result, err := server.LinkProject(context.Background(), "site", "/workspace/site")
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != "site" || projects.linkedName != "site" || projects.linkedPath != "/workspace/site" || result.Revision != 12 {
		t.Fatalf("comando não atravessou a composição explícita: project=%#v port=%#v result=%#v", project, projects, result)
	}

	read, err := server.QueryConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if read.Revision != 19 {
		t.Fatalf("consulta não atravessou a composição explícita: %#v", read)
	}
}
