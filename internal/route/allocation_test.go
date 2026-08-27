package route

import (
	"errors"
	"sync"
	"testing"
)

func intPtr(value int) *int { return &value }

func TestAllocateReusesPathIdentityWhenProjectOrderChanges(t *testing.T) {
	first, err := Allocate(Input{
		BasePort:  8080,
		PortCount: 3,
		Projects: []Project{
			{Name: "zeta", Path: "/sites/zeta"},
			{Name: "alpha", Path: "/sites/alpha"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Assigned["/sites/alpha"] != 8080 || first.Assigned["/sites/zeta"] != 8081 {
		t.Fatalf("alocação determinística inesperada: %#v", first.Assigned)
	}

	second, err := Allocate(Input{
		BasePort:    8080,
		PortCount:   3,
		Allocations: first.Allocations,
		Projects: []Project{
			{Name: "alpha", Path: "/sites/alpha"},
			{Name: "zeta", Path: "/sites/zeta"},
			{Name: "new", Path: "/sites/new"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Assigned["/sites/alpha"] != 8080 || second.Assigned["/sites/zeta"] != 8081 || second.Assigned["/sites/new"] != 8082 {
		t.Fatalf("reordenação alterou a identidade: %#v", second.Assigned)
	}
}

func TestAllocateReservesInfrastructureOverridesAndExternalListeners(t *testing.T) {
	override := 8090
	plan, err := Allocate(Input{
		BasePort:          8080,
		PortCount:         4,
		ReservedPorts:     []int{8080, 8082, 3210},
		ExternalListeners: []int{8081},
		Projects: []Project{
			{Name: "custom", Path: "/sites/custom", Override: &override},
			{Name: "automatic", Path: "/sites/automatic"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Assigned["/sites/custom"] != 8090 || plan.Assigned["/sites/automatic"] != 8083 {
		t.Fatalf("reservas não foram respeitadas: %#v", plan.Assigned)
	}
}

func TestAllocateFailsWithoutPartialPlanOnConflictOrExhaustion(t *testing.T) {
	conflicting := 8080
	_, err := Allocate(Input{
		BasePort:      8080,
		PortCount:     2,
		ReservedPorts: []int{8080},
		Projects: []Project{
			{Name: "one", Path: "/sites/one", Override: &conflicting},
		},
	})
	if !errors.Is(err, ErrPortConflict) {
		t.Fatalf("esperava conflito, obtido %v", err)
	}

	_, err = Allocate(Input{
		BasePort:      8080,
		PortCount:     1,
		ReservedPorts: []int{8080},
		Projects:      []Project{{Name: "one", Path: "/sites/one"}},
	})
	if !errors.Is(err, ErrPoolExhausted) {
		t.Fatalf("esperava exaustão, obtido %v", err)
	}
}

func TestAllocateIsSafeForConcurrentSnapshots(t *testing.T) {
	input := Input{
		BasePort:  8080,
		PortCount: 20,
		Projects: []Project{
			{Name: "alpha", Path: "/sites/alpha"},
			{Name: "beta", Path: "/sites/beta"},
		},
	}
	const workers = 16
	plans := make(chan Plan, workers)
	errorsCh := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer group.Done()
			plan, err := Allocate(input)
			if err != nil {
				errorsCh <- err
				return
			}
			plans <- plan
		}()
	}
	group.Wait()
	close(plans)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
	for plan := range plans {
		if plan.Assigned["/sites/alpha"] != 8080 || plan.Assigned["/sites/beta"] != 8081 {
			t.Fatalf("snapshot concorrente instável: %#v", plan.Assigned)
		}
	}
}

func TestOrphanPathsKeepParkChildren(t *testing.T) {
	orphans, err := OrphanPaths(map[string]int{
		"/sites/linked":                   8080,
		"/sites/park/temporarily-missing": 8081,
		"/sites/removed":                  8082,
		"/sites/park/nested/old":          8083,
	}, []string{"/sites/linked"}, []string{"/sites/park"})
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 2 || orphans[0] != "/sites/park/nested/old" || orphans[1] != "/sites/removed" {
		t.Fatalf("órfãos inesperados: %#v", orphans)
	}
}

func FuzzAllocateNeverReturnsDuplicateAssignedPorts(f *testing.F) {
	f.Add(8080, 5, 3)
	f.Add(10000, 1, 1)
	f.Fuzz(func(t *testing.T, base, count, projectCount int) {
		if base < 1024 {
			base = 1024
		}
		base = 1024 + (base-1024)%64000
		if count < 1 {
			count = 1
		}
		if count > 100 {
			count = 100
		}
		if projectCount < 0 {
			projectCount = -projectCount
		}
		projectCount %= 20
		projects := make([]Project, projectCount)
		for i := range projects {
			projects[i] = Project{Name: "project", Path: "/sites/project-" + string(rune('a'+i))}
		}
		plan, err := Allocate(Input{BasePort: base, PortCount: count, Projects: projects})
		if err != nil {
			return
		}
		seen := map[int]bool{}
		for _, port := range plan.Assigned {
			if seen[port] {
				t.Fatalf("porta duplicada %d: %#v", port, plan.Assigned)
			}
			seen[port] = true
		}
	})
}
