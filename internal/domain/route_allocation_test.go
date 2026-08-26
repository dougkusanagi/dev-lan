package domain

import "testing"

func TestConfigUsesPersistedRouteAllocationByPath(t *testing.T) {
	cfg := NewConfig()
	cfg.RoutePortAllocations = map[string]int{"/sites/alpha": 8123}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	project := Project{Name: "alpha", Path: "/sites/alpha"}
	if got := cfg.EffectiveRoutePort(project); got != 8123 {
		t.Fatalf("porta persistida não foi reutilizada: %d", got)
	}
}

func TestConfigRejectsRouteAllocationInfrastructureConflicts(t *testing.T) {
	for name, port := range map[string]int{
		"http":  80,
		"https": 443,
		"wsl":   8181,
		"ui":    3210,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := NewConfig()
			cfg.RoutePortAllocations = map[string]int{"/sites/unsafe": port}
			if err := cfg.Normalize(); err == nil {
				t.Fatalf("alocação na porta %d deveria ser rejeitada", port)
			}
		})
	}
}

func TestConfigRejectsUIPortConflictsWithProjectAndRuntimePorts(t *testing.T) {
	mode := ModeDev
	devPort := 3211
	routePort := 3212
	for name, project := range map[string]Project{
		"dev explícito":  {Name: "dev", Path: "/sites/dev", Mode: &mode, DevPort: &devPort},
		"rota explícita": {Name: "route", Path: "/sites/route", RoutePort: &routePort},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := NewConfig()
			cfg.UIPort = devPort
			if name == "rota explícita" {
				cfg.UIPort = routePort
			}
			cfg.Projects = []Project{project}
			if err := cfg.Normalize(); err == nil {
				t.Fatal("conflito com a porta administrativa deveria ser rejeitado")
			}
		})
	}
}
