package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dougkusanagi/dev-lan/internal/detect"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

type characterizationDevManager struct {
	started  []string
	stopped  []string
	restarts []string
}

func (m *characterizationDevManager) StartDev(_ context.Context, project domain.Project, _ int, _ string) error {
	m.started = append(m.started, project.Name)
	return nil
}

func (m *characterizationDevManager) StopDev(_ context.Context, project domain.Project, _ int) error {
	m.stopped = append(m.stopped, project.Name)
	return nil
}

func (m *characterizationDevManager) RestartDev(_ context.Context, project domain.Project, _ int, _ string) error {
	m.restarts = append(m.restarts, project.Name)
	return nil
}

func (m *characterizationDevManager) Status(_ context.Context, project domain.Project, port int) (platform.DevProcessStatus, error) {
	return platform.DevProcessStatus{ProjectName: project.Name, Port: port, State: platform.StateStopped}, nil
}

func (m *characterizationDevManager) InstallDeps(context.Context, domain.Project, string) (string, error) {
	return "", nil
}

func (m *characterizationDevManager) Build(context.Context, domain.Project, string) (string, error) {
	return "", nil
}

func (m *characterizationDevManager) Logs(context.Context, domain.Project, int) (string, error) {
	return "", nil
}

type characterizationToggleRunner struct {
	failAfter int
	calls     int
}

func (r *characterizationToggleRunner) Run(context.Context, ...string) (string, error) {
	r.calls++
	if r.failAfter > 0 && r.calls > r.failAfter {
		return "", errors.New("Caddy indisponível durante reload")
	}
	return "", nil
}

func TestCriticalAppFlowsCharacterization(t *testing.T) {
	t.Setenv("DEVLAN_TEST_MOCK", "1")
	ctx := context.Background()
	service := New(t.TempDir())
	service.Detector = detect.Detector{Inspector: detect.StaticInspector{
		Files: map[string]bool{
			"/workspace/site/package.json":   true,
			"/workspace/site/vite.config.ts": true,
		},
		FileContents: map[string]string{
			"/workspace/site/package.json": `{"name":"site","scripts":{"dev":"vite","build":"vite build"}}`,
		},
	}}
	service.Caddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	service.Dev = &characterizationDevManager{}

	project, result, err := service.Link(ctx, "site", "/workspace/site")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if project.Name != "site" || result.Revision == 0 {
		t.Fatalf("resultado de link inesperado: project=%#v result=%#v", project, result)
	}

	if err := service.StartDev(ctx, "site"); err != nil {
		t.Fatalf("start dev: %v", err)
	}
	if err := service.StopDev(ctx, "site"); err != nil {
		t.Fatalf("stop dev: %v", err)
	}
	manager := service.Dev.(*characterizationDevManager)
	if strings.Join(manager.started, ",") != "site" || strings.Join(manager.stopped, ",") != "site" {
		t.Fatalf("lifecycle dev não foi encaminhado: started=%v stopped=%v", manager.started, manager.stopped)
	}

	if _, err := service.Reload(ctx); err != nil {
		t.Fatalf("reload: %v", err)
	}
	topology := service.Topology(ctx)
	if topology.Error != "" {
		t.Fatalf("topology retornou erro: %s", topology.Error)
	}

	exported, err := service.ExportConfig()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(string(exported), `"format": "devlan-config"`) {
		t.Fatalf("envelope de exportação inesperado: %s", exported)
	}
	imported := New(t.TempDir())
	imported.Caddy = platform.CaddyClient{Runner: successfulRunner{}, WSL: true}
	// Keep this characterization independent from host ACLs: the static
	// inspector is the same deterministic adapter used by the link flow.
	imported.Detector = detect.Detector{Inspector: detect.StaticInspector{}}
	if err := imported.Store.Save(domain.NewConfig()); err != nil {
		t.Fatalf("preparar configuração para import: %v", err)
	}
	if _, err := imported.ImportConfig(ctx, exported); err != nil {
		t.Fatalf("import: %v", err)
	}
	importedConfig, err := imported.Config()
	if err != nil || len(importedConfig.Projects) != 1 || importedConfig.Projects[0].Name != "site" {
		t.Fatalf("configuração importada inesperada: cfg=%#v err=%v", importedConfig, err)
	}

	// Let staging/validation pass, then fail the live reload so the app must
	// restore the previously committed configuration and generated artifacts.
	runner := &characterizationToggleRunner{failAfter: 3}
	service.Caddy = platform.CaddyClient{Runner: runner, WSL: true}
	rollbackResult, reloadErr := service.Reload(ctx)
	if reloadErr == nil || rollbackResult.Status != "rolled_back" {
		t.Fatalf("falha de reload não preservou rollback: result=%#v err=%v", rollbackResult, reloadErr)
	}
	rolledBack, err := service.Config()
	if err != nil || len(rolledBack.Projects) != 1 || rolledBack.Projects[0].Name != "site" {
		t.Fatalf("rollback perdeu configuração anterior: cfg=%#v err=%v", rolledBack, err)
	}

	uninstall := New(t.TempDir())
	if err := uninstall.Store.Save(domain.NewConfig()); err != nil {
		t.Fatal(err)
	}
	if err := uninstall.ensureInstallationManifest(ctx); err != nil {
		t.Fatalf("preparar manifesto de uninstall: %v", err)
	}
	plan, err := uninstall.PlanUninstall(ctx, UninstallOptions{DryRun: true})
	if err != nil || !plan.Manifest {
		t.Fatalf("plan de uninstall não caracterizado: plan=%#v err=%v", plan, err)
	}
}
