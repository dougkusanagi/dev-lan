// Package route contains the pure route-port allocation policy.
//
// It deliberately has no filesystem, process or network dependencies. The
// application supplies the persisted allocations, ports owned by runtimes and
// listeners observed on the host; this package returns a complete plan that
// can be committed atomically by the caller.
package route

import (
	"errors"
	"fmt"
	pathpkg "path"
	"sort"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

var (
	ErrPoolExhausted = errors.New("pool de portas de rota esgotado")
	ErrPortConflict  = errors.New("conflito de porta de rota")
)

// Project is the minimum route information needed by the allocator. Path is
// the durable identity; Name is used only to make diagnostics actionable.
type Project struct {
	Name     string
	Path     string
	Override *int
}

// Input is a snapshot of all state that can affect allocation. Allocate never
// mutates the supplied slices or map, so callers can safely reuse them after a
// plan has been created.
type Input struct {
	BasePort          int
	PortCount         int
	ReservedPorts     []int
	ExternalListeners []int
	Allocations       map[string]int
	Projects          []Project
}

// Plan contains the complete persisted automatic allocation map and the
// effective route port of every project in Input. Allocations for projects
// temporarily absent from a park are intentionally retained.
type Plan struct {
	Allocations map[string]int
	Assigned    map[string]int
}

// Allocate performs a deterministic, all-or-nothing allocation. Existing
// automatic assignments are reused first. New projects are sorted by their
// normalized path, so discovery order cannot change their ports.
func Allocate(input Input) (Plan, error) {
	base, end, err := validatePool(input.BasePort, input.PortCount)
	if err != nil {
		return Plan{}, err
	}

	projects, err := normalizeProjects(input.Projects)
	if err != nil {
		return Plan{}, err
	}

	allocations, err := normalizeAllocations(input.Allocations)
	if err != nil {
		return Plan{}, err
	}

	used := make(map[int]string, len(allocations)+len(input.ReservedPorts)+len(input.ExternalListeners))
	reserve := func(port int, owner string) error {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%w: porta inválida %d", ErrPortConflict, port)
		}
		if previous, exists := used[port]; exists && previous != owner {
			return fmt.Errorf("%w: porta %d reservada por %s e %s", ErrPortConflict, port, previous, owner)
		}
		used[port] = owner
		return nil
	}

	for _, port := range input.ReservedPorts {
		if err := reserve(port, "infraestrutura"); err != nil {
			return Plan{}, err
		}
	}
	for _, port := range input.ExternalListeners {
		if err := reserve(port, "listener externo"); err != nil {
			return Plan{}, err
		}
	}
	for path, port := range allocations {
		if err := reserve(port, "alocação "+path); err != nil {
			return Plan{}, err
		}
	}

	// Overrides are validated before automatic ports are assigned. An
	// override does not replace the automatic allocation kept in state; this
	// makes switching back to auto stable and prevents accidental reuse.
	assigned := make(map[string]int, len(projects))
	for _, project := range projects {
		if project.Override == nil {
			continue
		}
		port := *project.Override
		if port < 1024 || port > 65535 {
			return Plan{}, fmt.Errorf("%w: projeto %s usa porta inválida %d", ErrPortConflict, project.Name, port)
		}
		if owner, exists := used[port]; exists && owner != "alocação "+project.Path {
			return Plan{}, fmt.Errorf("%w: projeto %s não pode usar porta %d (%s)", ErrPortConflict, project.Name, port, owner)
		}
		used[port] = "override " + project.Path
		assigned[project.Path] = port
	}

	for _, project := range projects {
		if project.Override != nil {
			continue
		}
		if port, exists := allocations[project.Path]; exists {
			if owner := used[port]; owner != "alocação "+project.Path {
				return Plan{}, fmt.Errorf("%w: alocação de %s perdeu a porta %d para %s", ErrPortConflict, project.Path, port, owner)
			}
			assigned[project.Path] = port
			continue
		}

		found := 0
		for port := base; port <= end; port++ {
			if _, exists := used[port]; exists {
				continue
			}
			found = port
			break
		}
		if found == 0 {
			return Plan{}, fmt.Errorf("%w: projeto %s precisa de uma porta entre %d e %d", ErrPoolExhausted, project.Name, base, end)
		}
		allocations[project.Path] = found
		if err := reserve(found, "alocação "+project.Path); err != nil {
			return Plan{}, err
		}
		assigned[project.Path] = found
	}

	// Keep entries that belong to no currently active project. They are
	// deliberately not removed here; Prune is the explicit destructive action.
	return Plan{Allocations: allocations, Assigned: assigned}, nil
}

func validatePool(base, count int) (int, int, error) {
	if base < 1024 || base > 65535 {
		return 0, 0, fmt.Errorf("%w: base de portas inválida %d", ErrPortConflict, base)
	}
	if count < 1 {
		return 0, 0, fmt.Errorf("%w: quantidade de portas inválida %d", ErrPortConflict, count)
	}
	end := base + count - 1
	if end < base || end > 65535 {
		return 0, 0, fmt.Errorf("%w: pool %d-%d ultrapassa 65535", ErrPortConflict, base, end)
	}
	return base, end, nil
}

func normalizeProjects(projects []Project) ([]Project, error) {
	result := make([]Project, len(projects))
	seen := make(map[string]struct{}, len(projects))
	for index, project := range projects {
		path, err := domain.NormalizePath(project.Path)
		if err != nil {
			return nil, fmt.Errorf("projeto %q: %w", project.Name, err)
		}
		if _, exists := seen[path]; exists {
			return nil, fmt.Errorf("%w: caminho duplicado %s", ErrPortConflict, path)
		}
		seen[path] = struct{}{}
		project.Path = path
		result[index] = project
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func normalizeAllocations(input map[string]int) (map[string]int, error) {
	result := make(map[string]int, len(input))
	owners := make(map[int]string, len(input))
	for rawPath, port := range input {
		path, err := domain.NormalizePath(rawPath)
		if err != nil {
			return nil, fmt.Errorf("alocação %q: %w", rawPath, err)
		}
		if _, exists := result[path]; exists {
			return nil, fmt.Errorf("%w: caminho de alocação duplicado %s", ErrPortConflict, path)
		}
		if port < 1024 || port > 65535 {
			return nil, fmt.Errorf("%w: alocação %s usa porta inválida %d", ErrPortConflict, path, port)
		}
		if previous, exists := owners[port]; exists && previous != path {
			return nil, fmt.Errorf("%w: porta %d alocada para %s e %s", ErrPortConflict, port, previous, path)
		}
		owners[port] = path
		result[path] = port
	}
	return result, nil
}

// OrphanPaths returns allocations that are not linked and are not a direct
// child of an active park. An active park keeps an absent child allocation so
// a temporary discovery failure cannot change a URL.
func OrphanPaths(allocations map[string]int, linkedPaths []string, parks []string) ([]string, error) {
	linked, err := normalizePathSet(linkedPaths)
	if err != nil {
		return nil, err
	}
	parkPaths, err := normalizePathSet(parks)
	if err != nil {
		return nil, err
	}
	orphans := make([]string, 0)
	for rawPath := range allocations {
		path, err := domain.NormalizePath(rawPath)
		if err != nil {
			return nil, fmt.Errorf("alocação órfã %q: %w", rawPath, err)
		}
		if _, exists := linked[path]; exists || isParkChild(path, parkPaths) {
			continue
		}
		orphans = append(orphans, path)
	}
	sort.Strings(orphans)
	return orphans, nil
}

func normalizePathSet(paths []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path, err := domain.NormalizePath(raw)
		if err != nil {
			return nil, err
		}
		result[path] = struct{}{}
	}
	return result, nil
}

func isParkChild(path string, parks map[string]struct{}) bool {
	for park := range parks {
		if pathpkg.Dir(path) == park && path != park {
			return true
		}
	}
	return false
}

// EqualAllocations compares maps without depending on map iteration order.
func EqualAllocations(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for path, port := range left {
		if right[path] != port {
			return false
		}
	}
	return true
}

// FormatAllocations produces deterministic CLI-friendly output.
func FormatAllocations(allocations map[string]int) []string {
	paths := make([]string, 0, len(allocations))
	for path := range allocations {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		lines = append(lines, fmt.Sprintf("%s\t%d", path, allocations[path]))
	}
	return lines
}
