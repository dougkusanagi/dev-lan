package domain

import (
	"errors"
	"fmt"
	pathpkg "path"
	"regexp"
	"sort"
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
	Name   string `json:"name"`
	Path   string `json:"path"`
	Mode   *Mode  `json:"mode,omitempty"`
	Secure *bool  `json:"secure,omitempty"`
}

type Park struct {
	Path string `json:"path"`
	Mode *Mode  `json:"mode,omitempty"`
}

type Config struct {
	Version       int       `json:"version"`
	DefaultMode   Mode      `json:"default_mode"`
	LANAddress    string    `json:"lan_address"`
	WindowsPort   int       `json:"windows_port"`
	HTTPSPort     int       `json:"https_port"`
	TLSEnabled    bool      `json:"tls_enabled"`
	WSLPort       int       `json:"wsl_port"`
	PHPFPMOsocket string    `json:"php_fpm_socket"`
	Projects      []Project `json:"projects"`
	Parks         []Park    `json:"parks"`
}

type ResolvedProject struct {
	Project Project
	Mode    Mode
	Source  ModeSource
	Park    *Park
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

func NewConfig() Config {
	return Config{
		Version:       1,
		DefaultMode:   ModePHP,
		LANAddress:    "auto",
		WindowsPort:   80,
		HTTPSPort:     443,
		WSLPort:       8181,
		PHPFPMOsocket: "/run/php/php-fpm.sock",
		Projects:      []Project{},
		Parks:         []Park{},
	}
}

// Normalize applies defaults and canonicalizes values loaded from disk. It
// also validates all user-controlled fields before they reach a generated
// Caddyfile.
func (c *Config) Normalize() error {
	if c.Version == 0 {
		c.Version = 1
	}
	if c.DefaultMode == "" {
		c.DefaultMode = ModePHP
	}
	if c.LANAddress == "" {
		c.LANAddress = "auto"
	}
	if c.WindowsPort == 0 {
		c.WindowsPort = 80
	}
	if c.WSLPort == 0 {
		c.WSLPort = 8181
	}
	if c.HTTPSPort == 0 {
		c.HTTPSPort = 443
	}
	if c.PHPFPMOsocket == "" {
		c.PHPFPMOsocket = "/run/php/php-fpm.sock"
	}
	if !c.DefaultMode.Valid() {
		return fmt.Errorf("modo global inválido %q", c.DefaultMode)
	}
	if c.WindowsPort < 1 || c.WindowsPort > 65535 {
		return fmt.Errorf("porta Windows inválida: %d", c.WindowsPort)
	}
	if c.WSLPort < 1 || c.WSLPort > 65535 {
		return fmt.Errorf("porta WSL inválida: %d", c.WSLPort)
	}
	if c.HTTPSPort < 1 || c.HTTPSPort > 65535 {
		return fmt.Errorf("porta HTTPS inválida: %d", c.HTTPSPort)
	}
	if c.TLSEnabled && c.WindowsPort == c.HTTPSPort {
		return fmt.Errorf("portas HTTP e HTTPS não podem ser iguais: %d", c.WindowsPort)
	}
	if strings.TrimSpace(c.PHPFPMOsocket) == "" || !strings.HasPrefix(c.PHPFPMOsocket, "/") {
		return fmt.Errorf("socket PHP-FPM deve ser um caminho absoluto Linux: %q", c.PHPFPMOsocket)
	}

	seenNames := map[string]struct{}{}
	seenPaths := map[string]struct{}{}
	for i := range c.Projects {
		project := &c.Projects[i]
		name, err := NormalizeName(project.Name)
		if err != nil {
			return err
		}
		project.Name = name
		project.Path, err = NormalizePath(project.Path)
		if err != nil {
			return fmt.Errorf("projeto %q: %w", project.Name, err)
		}
		if _, exists := seenNames[project.Name]; exists {
			return fmt.Errorf("projeto duplicado: %q", project.Name)
		}
		if _, exists := seenPaths[project.Path]; exists {
			return fmt.Errorf("caminho já registrado: %q", project.Path)
		}
		seenNames[project.Name] = struct{}{}
		seenPaths[project.Path] = struct{}{}
		if err := validateOptionalMode(project.Mode); err != nil {
			return fmt.Errorf("projeto %q: %w", project.Name, err)
		}
	}

	seenParks := map[string]struct{}{}
	for i := range c.Parks {
		park := &c.Parks[i]
		var err error
		park.Path, err = NormalizePath(park.Path)
		if err != nil {
			return fmt.Errorf("park: %w", err)
		}
		if _, exists := seenParks[park.Path]; exists {
			return fmt.Errorf("park duplicado: %q", park.Path)
		}
		seenParks[park.Path] = struct{}{}
		if err := validateOptionalMode(park.Mode); err != nil {
			return fmt.Errorf("park %q: %w", park.Path, err)
		}
	}

	sort.Slice(c.Projects, func(i, j int) bool { return c.Projects[i].Name < c.Projects[j].Name })
	sort.Slice(c.Parks, func(i, j int) bool { return c.Parks[i].Path < c.Parks[j].Path })
	return nil
}

func validateOptionalMode(mode *Mode) error {
	if mode != nil && !mode.Valid() {
		return fmt.Errorf("modo inválido %q", *mode)
	}
	return nil
}

func NormalizeName(value string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(value))
	if !projectNamePattern.MatchString(name) {
		return "", fmt.Errorf("nome de projeto inválido %q: use letras minúsculas, números e hífen", value)
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
	if project.Mode != nil {
		return ResolvedProject{Project: project, Mode: *project.Mode, Source: SourceProject}, nil
	}

	var selected *Park
	for i := range c.Parks {
		if isDirectChild(c.Parks[i].Path, project.Path) && (selected == nil || len(c.Parks[i].Path) > len(selected.Path)) {
			selected = &c.Parks[i]
		}
	}
	if selected != nil && selected.Mode != nil {
		return ResolvedProject{Project: project, Mode: *selected.Mode, Source: SourcePark, Park: selected}, nil
	}
	return ResolvedProject{Project: project, Mode: c.DefaultMode, Source: SourceGlobal, Park: selected}, nil
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

func (c Config) Validate() error {
	copy := c
	return copy.Normalize()
}

func (r ResolvedProject) URL(host string, httpPort, httpsPort int, secure bool) string {
	address := host
	if address == "" || address == "auto" {
		address = "localhost"
	}
	if r.Secure(secure) {
		if httpsPort == 443 {
			return fmt.Sprintf("https://%s/%s", address, r.Project.Name)
		}
		return fmt.Sprintf("https://%s:%d/%s", address, httpsPort, r.Project.Name)
	}
	if httpPort == 80 {
		return fmt.Sprintf("http://%s/%s", address, r.Project.Name)
	}
	return fmt.Sprintf("http://%s:%d/%s", address, httpPort, r.Project.Name)
}

var ErrUnsupportedMode = errors.New("modo ainda não implementado no MVP")
