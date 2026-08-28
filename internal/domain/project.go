package domain

// Project and park are the project-registration domain aggregates.

import (
	"errors"
	"fmt"
	pathpkg "path"
	"regexp"
	"strings"
)

// Mode is the serving strategy for a registered project. Only php is
// executed by the MVP; the other values are part of the stable schema so the
// registry can evolve without breaking existing projects.
type Mode string

const (
	ModeAuto   Mode = "auto"
	ModePHP    Mode = "php"
	ModeDev    Mode = "dev"
	ModeStatic Mode = "static"
)

func (m Mode) Valid() bool {
	switch m {
	case ModeAuto, ModePHP, ModeDev, ModeStatic:
		return true
	default:
		return false
	}
}

func ParseMode(value string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(value)))
	if !mode.Valid() {
		return "", fmt.Errorf("modo inválido %q (use auto, php, dev ou static)", value)
	}
	return mode, nil
}

type ModeSource string

const (
	SourceProject ModeSource = "project"
	SourcePark    ModeSource = "park"
	SourceGlobal  ModeSource = "global"
)

type Project struct {
	Name                string               `json:"name"`
	Path                string               `json:"path"`
	Mode                *Mode                `json:"mode,omitempty"`
	Secure              *bool                `json:"secure,omitempty"`
	PHPVersion          *string              `json:"php_version,omitempty"`
	PHPPreset           *PHPPreset           `json:"php_preset,omitempty"`
	PHPIsolatedPool     *bool                `json:"php_isolated_pool,omitempty"`
	PHPFPMPool          *PHPFPMPoolConfig    `json:"php_fpm_pool,omitempty"`
	ComposerEnvironment *ComposerEnvironment `json:"composer_environment,omitempty"`
	StaticDir           *string              `json:"static_dir,omitempty"`
	SPAFallback         *bool                `json:"spa_fallback,omitempty"`
	DevCommand          *string              `json:"dev_command,omitempty"`
	DevPort             *int                 `json:"dev_port,omitempty"`
	DevFramework        *string              `json:"dev_framework,omitempty"`
	PackageManager      *string              `json:"package_manager,omitempty"`
	IdleTimeout         *string              `json:"idle_timeout,omitempty"`
	RoutePort           *int                 `json:"route_port,omitempty"`
	Allowlist           []string             `json:"allowlist,omitempty"`
	ExposedUntil        *string              `json:"exposed_until,omitempty"`
	AuthEnabled         *bool                `json:"auth_enabled,omitempty"`
	AuthUsers           []AuthUser           `json:"auth_users,omitempty"`
}

type Park struct {
	Path         string   `json:"path"`
	Mode         *Mode    `json:"mode,omitempty"`
	Allowlist    []string `json:"allowlist,omitempty"`
	IgnoredPaths []string `json:"ignored_paths,omitempty"`
}

type ResolvedProject struct {
	Project   Project
	Mode      Mode
	Source    ModeSource
	Park      *Park
	RoutePort int
}

func (r ResolvedProject) Secure(global bool) bool {
	if r.Project.Secure != nil {
		return *r.Project.Secure
	}
	return global
}

// SecureProject supports the legacy global TLS flag while allowing a config
// containing project-level preferences to opt in one project at a time.

func (c Config) SecureProject(project Project) bool {
	for _, candidate := range c.Projects {
		if candidate.Secure != nil {
			return project.Secure != nil && *project.Secure
		}
	}
	if project.Secure != nil {
		return *project.Secure
	}
	return c.TLSEnabled
}

var projectNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func validateOptionalMode(mode *Mode) error {
	if mode != nil && !mode.Valid() {
		return fmt.Errorf("modo inválido %q", *mode)
	}
	return nil
}

var reservedProjectNames = map[string]bool{
	"devlan":    true,
	"localhost": true,
	"api":       true,
}

func NormalizeName(value string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(value))
	if !projectNamePattern.MatchString(name) {
		return "", fmt.Errorf("nome de projeto inválido %q: use letras minúsculas, números e hífen", value)
	}
	if reservedProjectNames[name] {
		return "", fmt.Errorf("nome de projeto reservado: %q", value)
	}
	return name, nil
}

// NormalizePath uses slash separators for both Windows paths and WSL paths.
// Caddy receives Linux paths, while os.Stat accepts the resulting form for
// local Windows fixtures as well.

func NormalizePath(value string) (string, error) {
	clean := pathpkg.Clean(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
	if clean == "." || !isAbsolutePath(clean) {
		return "", fmt.Errorf("caminho deve ser absoluto: %q", value)
	}
	return clean, nil
}

func isAbsolutePath(value string) bool {
	if strings.HasPrefix(value, "/") {
		return true
	}
	return len(value) >= 3 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) && value[1] == ':' && value[2] == '/'
}

func (c *Config) Link(name, projectPath string) (Project, error) {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return Project{}, err
	}
	normalizedPath, err := NormalizePath(projectPath)
	if err != nil {
		return Project{}, err
	}
	if _, found := c.Project(normalizedName); found {
		return Project{}, fmt.Errorf("projeto já registrado: %s", normalizedName)
	}
	for _, project := range c.Projects {
		if project.Path == normalizedPath {
			return Project{}, fmt.Errorf("caminho já registrado pelo projeto %s", project.Name)
		}
	}
	project := Project{Name: normalizedName, Path: normalizedPath}
	c.Projects = append(c.Projects, project)
	return project, c.Normalize()
}

func (c *Config) Unlink(name string) (Project, error) {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return Project{}, err
	}
	for i, project := range c.Projects {
		if project.Name == normalizedName {
			c.Projects = append(c.Projects[:i], c.Projects[i+1:]...)
			return project, nil
		}
	}
	return Project{}, fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c Config) Project(name string) (Project, bool) {
	for _, project := range c.Projects {
		if project.Name == name {
			return project, true
		}
	}
	return Project{}, false
}

func (c *Config) Park(path string) (Park, error) {
	normalizedPath, err := NormalizePath(path)
	if err != nil {
		return Park{}, err
	}
	for _, park := range c.Parks {
		if park.Path == normalizedPath {
			return Park{}, fmt.Errorf("diretório já estacionado: %s", normalizedPath)
		}
	}
	park := Park{Path: normalizedPath}
	c.Parks = append(c.Parks, park)
	return park, c.Normalize()
}

func (c *Config) Unpark(path string) (Park, error) {
	normalizedPath, err := NormalizePath(path)
	if err != nil {
		return Park{}, err
	}
	for i, park := range c.Parks {
		if park.Path == normalizedPath {
			c.Parks = append(c.Parks[:i], c.Parks[i+1:]...)
			return park, nil
		}
	}
	return Park{}, fmt.Errorf("diretório não estacionado: %s", normalizedPath)
}

// IgnoreParkProject prevents a project discovered below a parked directory
// from being materialized in the effective project list. It does not alter
// the project files or the parked directory itself.

func (c *Config) IgnoreParkProject(projectPath string) error {
	normalizedProjectPath, err := NormalizePath(projectPath)
	if err != nil {
		return err
	}
	found := false
	for i := range c.Parks {
		if !isDirectChild(c.Parks[i].Path, normalizedProjectPath) {
			continue
		}
		found = true
		alreadyIgnored := false
		for _, ignoredPath := range c.Parks[i].IgnoredPaths {
			if ignoredPath == normalizedProjectPath {
				alreadyIgnored = true
				break
			}
		}
		if !alreadyIgnored {
			c.Parks[i].IgnoredPaths = append(c.Parks[i].IgnoredPaths, normalizedProjectPath)
		}
	}
	if !found {
		return fmt.Errorf("projeto não pertence a um diretório estacionado: %s", normalizedProjectPath)
	}
	return c.Normalize()
}

// UnignoreParkProject makes a previously hidden parked project discoverable
// again. It is intentionally idempotent when the path is already visible.

func (c *Config) UnignoreParkProject(projectPath string) error {
	normalizedProjectPath, err := NormalizePath(projectPath)
	if err != nil {
		return err
	}
	found := false
	for i := range c.Parks {
		if !isDirectChild(c.Parks[i].Path, normalizedProjectPath) {
			continue
		}
		found = true
		filtered := c.Parks[i].IgnoredPaths[:0]
		for _, ignoredPath := range c.Parks[i].IgnoredPaths {
			if ignoredPath != normalizedProjectPath {
				filtered = append(filtered, ignoredPath)
			}
		}
		c.Parks[i].IgnoredPaths = filtered
	}
	if !found {
		return fmt.Errorf("projeto não pertence a um diretório estacionado: %s", normalizedProjectPath)
	}
	return c.Normalize()
}

func (c *Config) SetDefaultMode(mode Mode) error {
	if !mode.Valid() {
		return fmt.Errorf("modo inválido: %s", mode)
	}
	c.DefaultMode = mode
	return c.Normalize()
}

func (c *Config) SetProjectMode(name string, mode *Mode) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			if err := validateOptionalMode(mode); err != nil {
				return err
			}
			c.Projects[i].Mode = mode
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c Config) Resolve(name string) (ResolvedProject, error) {
	project, found := c.Project(name)
	if !found {
		return ResolvedProject{}, fmt.Errorf("projeto não encontrado: %s", name)
	}
	routePort := c.EffectiveRoutePort(project)

	if project.Mode != nil {
		return ResolvedProject{
			Project:   project,
			Mode:      *project.Mode,
			Source:    SourceProject,
			RoutePort: routePort,
		}, nil
	}

	var selected *Park
	for i := range c.Parks {
		if isDirectChild(c.Parks[i].Path, project.Path) && (selected == nil || len(c.Parks[i].Path) > len(selected.Path)) {
			selected = &c.Parks[i]
		}
	}
	if selected != nil && selected.Mode != nil {
		return ResolvedProject{
			Project:   project,
			Mode:      *selected.Mode,
			Source:    SourcePark,
			Park:      selected,
			RoutePort: routePort,
		}, nil
	}
	return ResolvedProject{
		Project:   project,
		Mode:      c.DefaultMode,
		Source:    SourceGlobal,
		Park:      selected,
		RoutePort: routePort,
	}, nil
}

func isDirectChild(parent, child string) bool {
	parent = strings.TrimSuffix(parent, "/")
	if parent == "" {
		parent = "/"
	}
	if parent == "/" {
		child = strings.TrimPrefix(child, "/")
		return child != "" && !strings.Contains(child, "/")
	}
	prefix := parent + "/"
	if !strings.HasPrefix(child, prefix) {
		return false
	}
	relative := strings.TrimPrefix(child, prefix)
	return relative != "" && !strings.Contains(relative, "/")
}

func (c Config) ResolvedProjects() ([]ResolvedProject, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	resolved := make([]ResolvedProject, 0, len(c.Projects))
	for _, project := range c.Projects {
		item, err := c.Resolve(project.Name)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, item)
	}
	return resolved, nil
}

func (r ResolvedProject) URL(host string, httpPort, _ int, secure bool) string {
	address := host
	if address == "" || address == "auto" {
		address = "localhost"
	}
	port := r.RoutePort
	if port <= 0 {
		port = httpPort
	}
	if r.Secure(secure) {
		return fmt.Sprintf("https://%s:%d/", address, port)
	}
	return fmt.Sprintf("http://%s:%d/", address, port)
}

// LocalDevURL is deliberately independent from the LAN route. The .localhost
// suffix is resolved by the browser to the developer's own machine and keeps
// Vite HMR, cookies, and absolute asset URLs on one origin.

func LocalDevURL(projectName string) string {
	return fmt.Sprintf("https://%s.localhost/", projectName)
}

var ErrUnsupportedMode = errors.New("modo ainda não implementado")
