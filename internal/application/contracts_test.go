package application

import (
	"context"
	"errors"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

type fakeProjectCommands struct {
	linkedName string
	linkedPath string
}

func (f *fakeProjectCommands) Link(_ context.Context, name, path string) (domain.Project, ApplyResult, error) {
	f.linkedName = name
	f.linkedPath = path
	return domain.Project{Name: name, Path: path}, ApplyResult{Status: "applied", Revision: 4}, nil
}

func (f *fakeProjectCommands) Unlink(context.Context, string) (domain.Project, ApplyResult, error) {
	return domain.Project{}, ApplyResult{}, nil
}

func (f *fakeProjectCommands) Park(context.Context, string) (domain.Park, ApplyResult, error) {
	return domain.Park{}, ApplyResult{}, nil
}

func (f *fakeProjectCommands) Unpark(context.Context, string) (domain.Park, ApplyResult, error) {
	return domain.Park{}, ApplyResult{}, nil
}

func (f *fakeProjectCommands) IgnoreProject(context.Context, string) (ApplyResult, error) {
	return ApplyResult{}, nil
}

func (f *fakeProjectCommands) UnignoreProject(context.Context, string) (ApplyResult, error) {
	return ApplyResult{}, nil
}

func (f *fakeProjectCommands) SetDefaultMode(context.Context, domain.Mode) (ApplyResult, error) {
	return ApplyResult{}, nil
}

func (f *fakeProjectCommands) SetProjectMode(context.Context, string, *domain.Mode) (ApplyResult, error) {
	return ApplyResult{}, nil
}

type fakeSettingsCommands struct {
	settings GlobalSettings
}

func (f *fakeSettingsCommands) SaveGlobalSettings(_ context.Context, settings GlobalSettings) (ApplyResult, error) {
	f.settings = settings
	return ApplyResult{Status: "applied"}, nil
}

type fakeQueries struct {
	cfg domain.Config
}

func (f fakeQueries) Config() (domain.Config, error) { return f.cfg, nil }

func (f fakeQueries) EffectiveConfig(_ context.Context, cfg domain.Config) (domain.Config, error) {
	return cfg, nil
}

func (f fakeQueries) Revision() uint64 { return f.cfg.Revision }

func TestCommandsUsePrivatePortsAndCommandDTOs(t *testing.T) {
	projects := &fakeProjectCommands{}
	settings := &fakeSettingsCommands{}
	commands := NewCommands(projects, settings)

	project, result, err := commands.LinkProject(context.Background(), LinkProjectCommand{Name: "site", Path: "/workspace/site"})
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != "site" || projects.linkedPath != "/workspace/site" || result.Revision != 4 {
		t.Fatalf("comando não encaminhado corretamente: project=%#v port=%#v result=%#v", project, projects, result)
	}

	settingsResult, err := commands.SaveGlobalSettings(context.Background(), GlobalSettings{DefaultMode: "php", TLSEnabled: true})
	if err != nil || settingsResult.Status != "applied" || settings.settings.DefaultMode != "php" || !settings.settings.TLSEnabled {
		t.Fatalf("configuração não encaminhada corretamente: result=%#v settings=%#v err=%v", settingsResult, settings.settings, err)
	}
}

func TestCommandsAndQueriesFailClosedWhenUnavailableOrCanceled(t *testing.T) {
	var nilCommands *Commands
	if _, _, err := nilCommands.LinkProject(context.Background(), LinkProjectCommand{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("comando nil deveria falhar fechado: %v", err)
	}

	queries := NewQueries(fakeQueries{cfg: domain.NewConfig()})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := queries.Config(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("query cancelada deveria preservar cancelamento: %v", err)
	}
}
