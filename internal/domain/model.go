package domain

import (
	"errors"
	"fmt"
	"net"
	pathpkg "path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	ProtocolVersion = 1
	CoreVersion     = "0.0.1"
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

type AuthUser struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
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

type Config struct {
	Version int `json:"version"`
	// Revision is the monotonic persisted revision, distinct from Version
	// (the schema version). It is used for optimistic concurrency control.
	Revision       uint64 `json:"revision,omitempty"`
	DefaultMode    Mode   `json:"default_mode"`
	RouteBasePort  int    `json:"route_base_port,omitempty"`
	RoutePortCount int    `json:"route_port_count,omitempty"`
	// RoutePortAllocations is state, not a user-facing preference. It is kept
	// in the same in-memory aggregate so the application can commit it together
	// with config.toml and state.json.
	RoutePortAllocations map[string]int     `json:"route_port_allocations,omitempty"`
	LANAddress           string             `json:"lan_address"`
	WindowsPort          int                `json:"windows_port"`
	HTTPSPort            int                `json:"https_port"`
	UIPort               int                `json:"ui_port,omitempty"`
	TLSEnabled           bool               `json:"tls_enabled"`
	WSLPort              int                `json:"wsl_port"`
	PHPFPMOsocket        string             `json:"php_fpm_socket"`
	PHPDefaultVersion    string             `json:"php_default_version"`
	PHPVersions          []PHPVersionConfig `json:"php_versions"`
	PHPFPMPool           PHPFPMPoolConfig   `json:"php_fpm_pool"`
	Composer             ComposerConfig     `json:"composer"`
	DevBasePort          int                `json:"dev_base_port,omitempty"`
	DefaultIdleTimeout   string             `json:"default_idle_timeout,omitempty"`
	Allowlist            []string           `json:"allowlist,omitempty"`
	AuthUsers            []AuthUser         `json:"auth_users,omitempty"`
	Projects             []Project          `json:"projects"`
	Parks                []Park             `json:"parks"`
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

// PHPPreset controls the small amount of framework-specific behavior that is
// needed to publish a PHP application. It is deliberately an allowlist: a
// preset never executes a project script or reads arbitrary configuration.
type PHPPreset string

const (
	PHPPresetLaravel PHPPreset = "laravel"
	PHPPresetSymfony PHPPreset = "symfony"
	PHPPresetGeneric PHPPreset = "generic"
)

func (p PHPPreset) Valid() bool {
	switch p {
	case PHPPresetLaravel, PHPPresetSymfony, PHPPresetGeneric:
		return true
	default:
		return false
	}
}

func (p PHPPreset) String() string { return string(p) }

func ParsePHPPreset(value string) (PHPPreset, error) {
	preset := PHPPreset(strings.ToLower(strings.TrimSpace(value)))
	if !preset.Valid() {
		return "", fmt.Errorf("preset PHP inválido %q (use laravel, symfony ou generic)", value)
	}
	return preset, nil
}

// PHPFPMPoolConfig is shared by all pools unless a project overrides it. The
// string duration is used instead of time.Duration so JSON/state files remain
// readable and stable across architectures.
type PHPFPMPoolConfig struct {
	MaxChildren int    `json:"max_children"`
	IdleTimeout string `json:"idle_timeout"`
	MaxRequests int    `json:"max_requests"`
}

func DefaultPHPFPMPoolConfig() PHPFPMPoolConfig {
	return PHPFPMPoolConfig{MaxChildren: 10, IdleTimeout: "10s", MaxRequests: 500}
}

func (p *PHPFPMPoolConfig) Normalize() error {
	defaults := DefaultPHPFPMPoolConfig()
	if p.MaxChildren == 0 {
		p.MaxChildren = defaults.MaxChildren
	}
	if p.IdleTimeout == "" {
		p.IdleTimeout = defaults.IdleTimeout
	}
	if p.MaxRequests == 0 {
		p.MaxRequests = defaults.MaxRequests
	}
	if p.MaxChildren < 1 || p.MaxChildren > 10000 {
		return fmt.Errorf("pm.max_children inválido: %d", p.MaxChildren)
	}
	duration, err := time.ParseDuration(p.IdleTimeout)
	if err != nil || duration <= 0 {
		return fmt.Errorf("pm.process_idle_timeout inválido: %q", p.IdleTimeout)
	}
	if p.MaxRequests < 0 || p.MaxRequests > 1000000 {
		return fmt.Errorf("pm.max_requests inválido: %d", p.MaxRequests)
	}
	return nil
}

func (p PHPFPMPoolConfig) IsZero() bool {
	return p.MaxChildren == 0 && p.IdleTimeout == "" && p.MaxRequests == 0
}

type ComposerEnvironment string

const (
	ComposerPerVersion ComposerEnvironment = "per-version"
	ComposerSystem     ComposerEnvironment = "system"
	ComposerAuto       ComposerEnvironment = "auto"
)

func (e ComposerEnvironment) Valid() bool {
	switch e {
	case ComposerPerVersion, ComposerSystem, ComposerAuto:
		return true
	default:
		return false
	}
}

func ParseComposerEnvironment(value string) (ComposerEnvironment, error) {
	environment := ComposerEnvironment(strings.ToLower(strings.TrimSpace(value)))
	if !environment.Valid() {
		return "", fmt.Errorf("ambiente do Composer inválido %q (use auto, system ou per-version)", value)
	}
	return environment, nil
}

type ComposerConfig struct {
	Environment ComposerEnvironment `json:"environment"`
	Binary      string              `json:"binary"`
}

type PHPVersionConfig struct {
	Version        string           `json:"version"`
	Extensions     []string         `json:"extensions,omitempty"`
	Socket         string           `json:"socket,omitempty"`
	PHPBinary      string           `json:"php_binary,omitempty"`
	FPMBinary      string           `json:"fpm_binary,omitempty"`
	ComposerBinary string           `json:"composer_binary,omitempty"`
	Pool           PHPFPMPoolConfig `json:"pool,omitempty"`
}

var phpVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
var phpExtensionPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.+-]{0,63}$`)

func NormalizePHPVersion(value string) (string, error) {
	version := strings.TrimSpace(value)
	if !phpVersionPattern.MatchString(version) {
		return "", fmt.Errorf("versão PHP inválida %q (use MAJOR.MINOR, por exemplo 8.5)", value)
	}
	return version, nil
}

func normalizePHPBinary(value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.ContainsAny(value, "\r\n\t ") || value == "." || value == ".." {
		return "", fmt.Errorf("%s inválido: %q", field, value)
	}
	return value, nil
}

func (v *PHPVersionConfig) Normalize() error {
	version, err := NormalizePHPVersion(v.Version)
	if err != nil {
		return err
	}
	v.Version = version
	if v.Socket != "" {
		v.Socket, err = NormalizeLinuxAbsolutePath(v.Socket, "socket PHP-FPM")
		if err != nil {
			return err
		}
	}
	if v.Pool.IsZero() {
		v.Pool = PHPFPMPoolConfig{}
	} else if err := v.Pool.Normalize(); err != nil {
		return fmt.Errorf("PHP %s: %w", v.Version, err)
	}
	for i, extension := range v.Extensions {
		extension = strings.ToLower(strings.TrimSpace(extension))
		if !phpExtensionPattern.MatchString(extension) {
			return fmt.Errorf("extensão PHP inválida %q na versão %s", v.Extensions[i], v.Version)
		}
		v.Extensions[i] = extension
	}
	sort.Strings(v.Extensions)
	v.Extensions = uniqueStrings(v.Extensions)
	if v.PHPBinary, err = normalizePHPBinary(v.PHPBinary, "php_binary"); err != nil {
		return err
	}
	if v.FPMBinary, err = normalizePHPBinary(v.FPMBinary, "fpm_binary"); err != nil {
		return err
	}
	if v.ComposerBinary, err = normalizePHPBinary(v.ComposerBinary, "composer_binary"); err != nil {
		return err
	}
	return nil
}

func NormalizeLinuxAbsolutePath(value, label string) (string, error) {
	clean := pathpkg.Clean(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
	if clean == "." || !strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("%s deve ser um caminho absoluto Linux: %q", label, value)
	}
	return clean, nil
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func NormalizeCIDR(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errors.New("CIDR ou endereço IP não pode ser vazio")
	}
	candidate := trimmed
	if !strings.Contains(candidate, "/") {
		if strings.Contains(candidate, ":") {
			candidate += "/128"
		} else {
			candidate += "/32"
		}
	}
	_, ipNet, err := net.ParseCIDR(candidate)
	if err != nil {
		return "", fmt.Errorf("CIDR ou endereço IP inválido: %q", value)
	}
	return ipNet.String(), nil
}

func NewConfig() Config {
	return Config{
		Version:           1,
		DefaultMode:       ModePHP,
		RouteBasePort:     8080,
		RoutePortCount:    100,
		LANAddress:        "auto",
		WindowsPort:       80,
		HTTPSPort:         443,
		UIPort:            3210,
		WSLPort:           8181,
		PHPFPMOsocket:     "/run/php/php-fpm.sock",
		PHPDefaultVersion: "8.5",
		PHPFPMPool:        DefaultPHPFPMPoolConfig(),
		Composer:          ComposerConfig{Environment: ComposerPerVersion, Binary: "composer"},
		Allowlist:         []string{},
		AuthUsers:         []AuthUser{},
		Projects:          []Project{},
		Parks:             []Park{},
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
	if c.RouteBasePort == 0 {
		c.RouteBasePort = 8080
	}
	if c.RoutePortCount == 0 {
		c.RoutePortCount = 100
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
	if c.UIPort == 0 {
		c.UIPort = 3210
	}
	if c.PHPFPMOsocket == "" {
		c.PHPFPMOsocket = "/run/php/php-fpm.sock"
	}
	if c.PHPDefaultVersion == "" {
		c.PHPDefaultVersion = "8.5"
	}
	if c.PHPFPMPool.IsZero() {
		c.PHPFPMPool = DefaultPHPFPMPoolConfig()
	}
	if c.Composer.Environment == "" {
		c.Composer.Environment = ComposerPerVersion
	}
	if c.Composer.Binary == "" {
		c.Composer.Binary = "composer"
	}
	if !c.DefaultMode.Valid() {
		return fmt.Errorf("modo global inválido %q", c.DefaultMode)
	}
	if c.RouteBasePort < 1024 || c.RouteBasePort > 65535 {
		return fmt.Errorf("porta base de rota inválida: %d", c.RouteBasePort)
	}
	if c.RoutePortCount < 1 || c.RoutePortCount > 65535-c.RouteBasePort+1 {
		return fmt.Errorf("quantidade de portas de rota inválida: %d", c.RoutePortCount)
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
	if c.UIPort < 1 || c.UIPort > 65535 {
		return fmt.Errorf("porta administrativa inválida: %d", c.UIPort)
	}
	for name, port := range map[string]int{"HTTP": c.WindowsPort, "HTTPS": c.HTTPSPort, "WSL": c.WSLPort} {
		if c.UIPort == port {
			return fmt.Errorf("porta administrativa %d conflita com %s", c.UIPort, name)
		}
	}
	if c.UIPort >= c.RouteBasePort && c.UIPort < c.RouteBasePort+c.RoutePortCount {
		return fmt.Errorf("porta administrativa %d está dentro do pool de rotas %d-%d", c.UIPort, c.RouteBasePort, c.RouteBasePort+c.RoutePortCount-1)
	}
	if c.TLSEnabled && c.WindowsPort == c.HTTPSPort {
		return fmt.Errorf("portas HTTP e HTTPS não podem ser iguais: %d", c.WindowsPort)
	}
	if strings.TrimSpace(c.PHPFPMOsocket) == "" || !strings.HasPrefix(c.PHPFPMOsocket, "/") {
		return fmt.Errorf("socket PHP-FPM deve ser um caminho absoluto Linux: %q", c.PHPFPMOsocket)
	}
	if _, err := NormalizePHPVersion(c.PHPDefaultVersion); err != nil {
		return fmt.Errorf("versão PHP global: %w", err)
	}
	if err := c.PHPFPMPool.Normalize(); err != nil {
		return fmt.Errorf("pool PHP global: %w", err)
	}
	if !c.Composer.Environment.Valid() {
		return fmt.Errorf("ambiente do Composer inválido: %q", c.Composer.Environment)
	}
	if _, err := normalizePHPBinary(c.Composer.Binary, "composer_binary"); err != nil {
		return err
	}

	if c.RoutePortAllocations == nil {
		c.RoutePortAllocations = map[string]int{}
	} else {
		normalizedAllocations := make(map[string]int, len(c.RoutePortAllocations))
		seenAllocationPorts := make(map[int]string, len(c.RoutePortAllocations))
		for rawPath, port := range c.RoutePortAllocations {
			path, err := NormalizePath(rawPath)
			if err != nil {
				return fmt.Errorf("alocação de rota %q: %w", rawPath, err)
			}
			if port < 1024 || port > 65535 {
				return fmt.Errorf("alocação de rota %q usa porta inválida: %d", path, port)
			}
			if _, exists := normalizedAllocations[path]; exists {
				return fmt.Errorf("alocação de rota duplicada para %q", path)
			}
			if previous, exists := seenAllocationPorts[port]; exists && previous != path {
				return fmt.Errorf("porta de rota %d alocada para %q e %q", port, previous, path)
			}
			normalizedAllocations[path] = port
			seenAllocationPorts[port] = path
		}
		c.RoutePortAllocations = normalizedAllocations
	}

	for i, cidr := range c.Allowlist {
		norm, err := NormalizeCIDR(cidr)
		if err != nil {
			return fmt.Errorf("allowlist global: %w", err)
		}
		c.Allowlist[i] = norm
	}
	sort.Strings(c.Allowlist)
	c.Allowlist = uniqueStrings(c.Allowlist)

	for _, user := range c.AuthUsers {
		if strings.TrimSpace(user.Username) == "" || strings.TrimSpace(user.PasswordHash) == "" {
			return fmt.Errorf("usuário de autenticação global inválido")
		}
	}

	seenPHPVersions := map[string]struct{}{}
	for i := range c.PHPVersions {
		version := &c.PHPVersions[i]
		if err := version.Normalize(); err != nil {
			return err
		}
		if _, exists := seenPHPVersions[version.Version]; exists {
			return fmt.Errorf("versão PHP duplicada: %s", version.Version)
		}
		seenPHPVersions[version.Version] = struct{}{}
	}
	sort.Slice(c.PHPVersions, func(i, j int) bool { return c.PHPVersions[i].Version < c.PHPVersions[j].Version })
	if len(c.PHPVersions) > 0 {
		if _, found := seenPHPVersions[c.PHPDefaultVersion]; !found {
			// Configurations written by an early phase may contain the first
			// installed version without a matching global preference. Select the
			// stable first entry instead of making the whole registry unloadable.
			c.PHPDefaultVersion = c.PHPVersions[0].Version
		}
	}

	if c.DevBasePort == 0 {
		c.DevBasePort = 9100
	}
	if c.DefaultIdleTimeout == "" {
		c.DefaultIdleTimeout = "15m"
	}
	if c.DevBasePort < 1024 || c.DevBasePort > 65000 {
		return fmt.Errorf("porta base dev inválida: %d", c.DevBasePort)
	}
	if _, err := time.ParseDuration(c.DefaultIdleTimeout); err != nil {
		return fmt.Errorf("idle timeout padrão inválido: %q", c.DefaultIdleTimeout)
	}

	seenNames := map[string]struct{}{}
	seenPaths := map[string]struct{}{}
	seenDevPorts := map[int]string{}
	seenRoutePorts := map[int]string{}
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
		if project.RoutePort != nil {
			port := *project.RoutePort
			if port < 1024 || port > 65535 {
				return fmt.Errorf("projeto %q: porta de rota inválida: %d", project.Name, port)
			}
			if existingName, exists := seenRoutePorts[port]; exists {
				return fmt.Errorf("porta de rota %d em conflito entre %q e %q", port, existingName, project.Name)
			}
			seenRoutePorts[port] = project.Name
		}
		for j, cidr := range project.Allowlist {
			norm, err := NormalizeCIDR(cidr)
			if err != nil {
				return fmt.Errorf("projeto %q: allowlist: %w", project.Name, err)
			}
			project.Allowlist[j] = norm
		}
		sort.Strings(project.Allowlist)
		project.Allowlist = uniqueStrings(project.Allowlist)

		if project.ExposedUntil != nil && strings.TrimSpace(*project.ExposedUntil) != "" {
			if _, err := time.Parse(time.RFC3339, strings.TrimSpace(*project.ExposedUntil)); err != nil {
				return fmt.Errorf("projeto %q: exposed_until inválido (esperado RFC3339): %q", project.Name, *project.ExposedUntil)
			}
		}

		for _, user := range project.AuthUsers {
			if strings.TrimSpace(user.Username) == "" || strings.TrimSpace(user.PasswordHash) == "" {
				return fmt.Errorf("projeto %q: usuário de autenticação inválido", project.Name)
			}
		}

		if project.DevPort != nil {
			port := *project.DevPort
			if port < 1024 || port > 65535 {
				return fmt.Errorf("projeto %q: porta dev inválida: %d", project.Name, port)
			}
			if existingName, exists := seenDevPorts[port]; exists {
				return fmt.Errorf("porta dev %d em conflito entre %q e %q", port, existingName, project.Name)
			}
			seenDevPorts[port] = project.Name
		}
		if project.IdleTimeout != nil && *project.IdleTimeout != "" {
			if duration, err := time.ParseDuration(*project.IdleTimeout); err != nil || duration <= 0 {
				return fmt.Errorf("projeto %q: idle timeout inválido: %q", project.Name, *project.IdleTimeout)
			}
		}
		if project.StaticDir != nil && *project.StaticDir != "" {
			clean := pathpkg.Clean(strings.ReplaceAll(strings.TrimSpace(*project.StaticDir), "\\", "/"))
			clean = strings.TrimPrefix(clean, "./")
			*project.StaticDir = clean
		}
		if project.PHPVersion != nil {
			version, err := NormalizePHPVersion(*project.PHPVersion)
			if err != nil {
				return fmt.Errorf("projeto %q: %w", project.Name, err)
			}
			*project.PHPVersion = version
			if len(c.PHPVersions) > 0 {
				if _, found := seenPHPVersions[version]; !found {
					return fmt.Errorf("projeto %q referencia PHP não registrado: %s", project.Name, version)
				}
			}
		}
		if project.PHPPreset != nil && !project.PHPPreset.Valid() {
			return fmt.Errorf("projeto %q: preset PHP inválido %q", project.Name, *project.PHPPreset)
		}
		if project.PHPFPMPool != nil {
			if err := project.PHPFPMPool.Normalize(); err != nil {
				return fmt.Errorf("projeto %q: %w", project.Name, err)
			}
		}
		if project.ComposerEnvironment != nil && !project.ComposerEnvironment.Valid() {
			return fmt.Errorf("projeto %q: ambiente do Composer inválido %q", project.Name, *project.ComposerEnvironment)
		}
	}
	for path, port := range c.RoutePortAllocations {
		for name, reserved := range map[string]int{
			"HTTP":  c.WindowsPort,
			"HTTPS": c.HTTPSPort,
			"WSL":   c.WSLPort,
			"UI":    c.UIPort,
		} {
			if port == reserved {
				return fmt.Errorf("alocação de rota %q conflita com a porta %s %d", path, name, port)
			}
		}
		if owner, exists := seenRoutePorts[port]; exists {
			allowed := false
			for _, project := range c.Projects {
				if project.Path == path && project.Name == owner && project.RoutePort != nil && *project.RoutePort == port {
					allowed = true
					break
				}
			}
			if !allowed {
				return fmt.Errorf("alocação de rota %q conflita com o override do projeto %q na porta %d", path, owner, port)
			}
		}
	}
	if c.UIPort == c.DevBasePort {
		return fmt.Errorf("porta administrativa %d conflita com a base do runtime dev", c.UIPort)
	}
	for _, project := range c.Projects {
		if project.RoutePort != nil && *project.RoutePort == c.UIPort {
			return fmt.Errorf("porta administrativa %d conflita com a rota do projeto %q", c.UIPort, project.Name)
		}
		if project.DevPort != nil && *project.DevPort == c.UIPort {
			return fmt.Errorf("porta administrativa %d conflita com o runtime do projeto %q", c.UIPort, project.Name)
		}
		devPort := c.DevPort(project)
		if devPort == c.UIPort {
			return fmt.Errorf("porta administrativa %d conflita com o runtime do projeto %q", c.UIPort, project.Name)
		}
		backend := devPort + 10000
		if devPort > 55000 {
			backend = devPort - 1000
		}
		if backend == c.UIPort {
			return fmt.Errorf("porta administrativa %d conflita com o backend do runtime do projeto %q", c.UIPort, project.Name)
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
		for j, cidr := range park.Allowlist {
			norm, err := NormalizeCIDR(cidr)
			if err != nil {
				return fmt.Errorf("park %q: allowlist: %w", park.Path, err)
			}
			park.Allowlist[j] = norm
		}
		sort.Strings(park.Allowlist)
		park.Allowlist = uniqueStrings(park.Allowlist)
		seenIgnoredPaths := map[string]struct{}{}
		for j, ignoredPath := range park.IgnoredPaths {
			norm, err := NormalizePath(ignoredPath)
			if err != nil {
				return fmt.Errorf("park %q: projeto ignorado: %w", park.Path, err)
			}
			if _, exists := seenIgnoredPaths[norm]; exists {
				return fmt.Errorf("park %q: projeto ignorado duplicado: %q", park.Path, norm)
			}
			seenIgnoredPaths[norm] = struct{}{}
			park.IgnoredPaths[j] = norm
		}
		sort.Strings(park.IgnoredPaths)
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

func (c Config) PHPVersion(version string) (PHPVersionConfig, bool) {
	for _, configured := range c.PHPVersions {
		if configured.Version == version {
			return configured, true
		}
	}
	return PHPVersionConfig{}, false
}

func (c Config) EffectivePHPVersion(project Project) string {
	if project.PHPVersion != nil && strings.TrimSpace(*project.PHPVersion) != "" {
		return *project.PHPVersion
	}
	return c.PHPDefaultVersion
}

func (c Config) PHPProjectPreset(project Project) PHPPreset {
	if project.PHPPreset != nil && project.PHPPreset.Valid() {
		return *project.PHPPreset
	}
	// Laravel remains the compatibility default for projects registered before
	// presets were added to the state schema.
	return PHPPresetLaravel
}

func (c Config) PHPDocumentRoot(project Project) string {
	preset := c.PHPProjectPreset(project)
	if preset == PHPPresetLaravel || preset == PHPPresetSymfony {
		return pathpkg.Join(project.Path, "public")
	}
	return project.Path
}

func PHPSharedSocket(version string) string {
	return pathpkg.Join("/run/devlan/php", version, "shared.sock")
}

func PHPIsolatedSocket(version, projectName string) string {
	if projectName == "shared" {
		projectName = "shared-isolated"
	}
	return pathpkg.Join("/run/devlan/php", version, projectName+".sock")
}

func (c Config) PHPIsolated(project Project) bool {
	return project.PHPIsolatedPool != nil && *project.PHPIsolatedPool
}

func (c Config) PHPSocket(project Project) string {
	version := c.EffectivePHPVersion(project)
	if c.PHPIsolated(project) {
		return PHPIsolatedSocket(version, project.Name)
	}
	if configured, found := c.PHPVersion(version); found && configured.Socket != "" {
		return configured.Socket
	}
	if _, configured := c.PHPVersion(version); configured || project.PHPVersion != nil {
		return PHPSharedSocket(version)
	}
	// State created by the MVP has no PHPVersions entries and must continue to
	// use the socket configured by the original installer.
	return c.PHPFPMOsocket
}

func mergePHPFPMPool(base, override PHPFPMPoolConfig) PHPFPMPoolConfig {
	if override.MaxChildren != 0 {
		base.MaxChildren = override.MaxChildren
	}
	if override.IdleTimeout != "" {
		base.IdleTimeout = override.IdleTimeout
	}
	if override.MaxRequests != 0 {
		base.MaxRequests = override.MaxRequests
	}
	return base
}

func (c Config) PHPPool(project Project) PHPFPMPoolConfig {
	pool := c.PHPFPMPool
	if configured, found := c.PHPVersion(c.EffectivePHPVersion(project)); found {
		pool = mergePHPFPMPool(pool, configured.Pool)
	}
	if project.PHPFPMPool != nil {
		pool = mergePHPFPMPool(pool, *project.PHPFPMPool)
	}
	_ = pool.Normalize()
	return pool
}

func (c *Config) AddPHPVersion(version string, extensions []string) (PHPVersionConfig, error) {
	normalized, err := NormalizePHPVersion(version)
	if err != nil {
		return PHPVersionConfig{}, err
	}
	wasEmpty := len(c.PHPVersions) == 0
	for _, configured := range c.PHPVersions {
		if configured.Version == normalized {
			return PHPVersionConfig{}, fmt.Errorf("versão PHP já registrada: %s", normalized)
		}
	}
	entry := PHPVersionConfig{Version: normalized, Extensions: append([]string(nil), extensions...)}
	if err := entry.Normalize(); err != nil {
		return PHPVersionConfig{}, err
	}
	c.PHPVersions = append(c.PHPVersions, entry)
	if wasEmpty || c.PHPDefaultVersion == "" {
		c.PHPDefaultVersion = normalized
	}
	return entry, c.Normalize()
}

func (c *Config) RemovePHPVersion(version string) (PHPVersionConfig, error) {
	normalized, err := NormalizePHPVersion(version)
	if err != nil {
		return PHPVersionConfig{}, err
	}
	for i, configured := range c.PHPVersions {
		if configured.Version == normalized {
			c.PHPVersions = append(c.PHPVersions[:i], c.PHPVersions[i+1:]...)
			if c.PHPDefaultVersion == normalized {
				if len(c.PHPVersions) > 0 {
					c.PHPDefaultVersion = c.PHPVersions[0].Version
				} else {
					c.PHPDefaultVersion = "8.5"
				}
			}
			return configured, c.Normalize()
		}
	}
	return PHPVersionConfig{}, fmt.Errorf("versão PHP não registrada: %s", normalized)
}

func (c *Config) SetDefaultPHPVersion(version string) error {
	normalized, err := NormalizePHPVersion(version)
	if err != nil {
		return err
	}
	c.PHPDefaultVersion = normalized
	return c.Normalize()
}

func (c *Config) SetPHPVersionExtensions(version string, extensions []string) error {
	normalized, err := NormalizePHPVersion(version)
	if err != nil {
		return err
	}
	for i := range c.PHPVersions {
		if c.PHPVersions[i].Version == normalized {
			c.PHPVersions[i].Extensions = append([]string(nil), extensions...)
			return c.Normalize()
		}
	}
	return fmt.Errorf("versão PHP não registrada: %s", normalized)
}

func (c *Config) SetPHPVersionPool(version string, pool PHPFPMPoolConfig) error {
	normalized, err := NormalizePHPVersion(version)
	if err != nil {
		return err
	}
	if err := pool.Normalize(); err != nil {
		return err
	}
	for i := range c.PHPVersions {
		if c.PHPVersions[i].Version == normalized {
			c.PHPVersions[i].Pool = pool
			return c.Normalize()
		}
	}
	return fmt.Errorf("versão PHP não registrada: %s", normalized)
}

func (c *Config) SetProjectPHPVersion(name string, version *string) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	var normalizedVersion *string
	if version != nil {
		value, err := NormalizePHPVersion(*version)
		if err != nil {
			return err
		}
		normalizedVersion = &value
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].PHPVersion = normalizedVersion
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c *Config) SetProjectPHPPreset(name string, preset *PHPPreset) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	if preset != nil && !preset.Valid() {
		return fmt.Errorf("preset PHP inválido: %s", *preset)
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].PHPPreset = preset
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c *Config) SetProjectPHPIsolated(name string, isolated bool) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].PHPIsolatedPool = &isolated
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c *Config) SetProjectPHPPool(name string, pool *PHPFPMPoolConfig) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	if pool != nil {
		copy := *pool
		if err := copy.Normalize(); err != nil {
			return err
		}
		pool = &copy
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].PHPFPMPool = pool
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c *Config) SetProjectStaticDir(name string, dir *string) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].StaticDir = dir
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c *Config) SetProjectSPAFallback(name string, spa *bool) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].SPAFallback = spa
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c *Config) SetProjectDevPort(name string, port *int) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].DevPort = port
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c *Config) SetProjectDevCommand(name string, cmd *string) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].DevCommand = cmd
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c *Config) SetProjectPackageManager(name string, pm *string) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].PackageManager = pm
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c *Config) SetProjectIdleTimeout(name string, timeout *string) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].IdleTimeout = timeout
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c Config) StaticDocumentRoot(project Project) string {
	if project.StaticDir != nil && strings.TrimSpace(*project.StaticDir) != "" {
		dir := strings.TrimSpace(*project.StaticDir)
		if strings.HasPrefix(dir, "/") {
			return dir
		}
		return pathpkg.Join(project.Path, dir)
	}
	return project.Path
}

func (c Config) SPAFallback(project Project) bool {
	if project.SPAFallback != nil {
		return *project.SPAFallback
	}
	return true
}

func (c Config) DevPort(project Project) int {
	if project.DevPort != nil && *project.DevPort > 0 {
		return *project.DevPort
	}
	// Deterministic port allocation: find index of project among registered projects
	base := c.DevBasePort
	if base == 0 {
		base = 9100
	}
	allocated := map[int]bool{}
	for _, p := range c.Projects {
		if p.DevPort != nil && *p.DevPort > 0 {
			allocated[*p.DevPort] = true
		}
	}
	for index, p := range c.Projects {
		if p.Name == project.Name {
			candidate := base + index
			for allocated[candidate] {
				candidate++
			}
			return candidate
		}
	}
	return base
}

func (c Config) DevCommand(project Project) string {
	if project.DevCommand != nil && strings.TrimSpace(*project.DevCommand) != "" {
		return strings.TrimSpace(*project.DevCommand)
	}
	pm := c.PackageManager(project)
	switch pm {
	case "yarn":
		return "yarn dev"
	case "pnpm":
		return "pnpm run dev"
	case "bun":
		return "bun run dev"
	default:
		return "npm run dev"
	}
}

func (c Config) PackageManager(project Project) string {
	if project.PackageManager != nil && strings.TrimSpace(*project.PackageManager) != "" {
		return strings.TrimSpace(*project.PackageManager)
	}
	return "npm"
}

func (c Config) ProjectIdleTimeout(project Project) time.Duration {
	if project.IdleTimeout != nil && strings.TrimSpace(*project.IdleTimeout) != "" {
		if d, err := time.ParseDuration(*project.IdleTimeout); err == nil && d > 0 {
			return d
		}
	}
	if c.DefaultIdleTimeout != "" {
		if d, err := time.ParseDuration(c.DefaultIdleTimeout); err == nil && d > 0 {
			return d
		}
	}
	return 15 * time.Minute
}

func (c Config) EffectiveRoutePort(project Project) int {
	if project.RoutePort != nil && *project.RoutePort > 0 {
		return *project.RoutePort
	}
	if port, found := c.RoutePortAllocations[project.Path]; found && port > 0 {
		return port
	}
	base := c.RouteBasePort
	if base == 0 {
		base = 8080
	}
	allocated := map[int]bool{
		c.WindowsPort: true,
		c.HTTPSPort:   true,
		c.WSLPort:     true,
		c.UIPort:      true,
	}
	for _, p := range c.Projects {
		if p.RoutePort != nil && *p.RoutePort > 0 {
			allocated[*p.RoutePort] = true
		}
		if p.DevPort != nil && *p.DevPort > 0 {
			allocated[*p.DevPort] = true
		}
	}
	for index, p := range c.Projects {
		if p.Name == project.Name {
			candidate := base + index
			for allocated[candidate] {
				candidate++
			}
			if c.RoutePortCount > 0 && candidate >= base+c.RoutePortCount {
				return 0
			}
			return candidate
		}
	}
	return base
}

func (c Config) EffectiveAllowlist(project Project) []string {
	if len(project.Allowlist) > 0 {
		return project.Allowlist
	}
	for i := range c.Parks {
		if isDirectChild(c.Parks[i].Path, project.Path) {
			if len(c.Parks[i].Allowlist) > 0 {
				return c.Parks[i].Allowlist
			}
		}
	}
	return c.Allowlist
}

func (c Config) EffectiveAuth(project Project) (bool, []AuthUser) {
	if project.AuthEnabled != nil {
		if !*project.AuthEnabled {
			return false, nil
		}
		if len(project.AuthUsers) > 0 {
			return true, project.AuthUsers
		}
		return len(c.AuthUsers) > 0, c.AuthUsers
	}
	if len(project.AuthUsers) > 0 {
		return true, project.AuthUsers
	}
	return len(c.AuthUsers) > 0, c.AuthUsers
}

func (c Config) IsExposureExpired(project Project, now time.Time) bool {
	if project.ExposedUntil == nil || strings.TrimSpace(*project.ExposedUntil) == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*project.ExposedUntil))
	if err != nil {
		return false
	}
	return now.After(t)
}

func (c *Config) SetProjectRoutePort(name string, port *int) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			if port == nil || *port <= 0 {
				c.Projects[i].RoutePort = nil
			} else {
				value := *port
				c.Projects[i].RoutePort = &value
			}
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c *Config) SetGlobalAllowlist(cidrs []string) error {
	c.Allowlist = append([]string(nil), cidrs...)
	return c.Normalize()
}

func (c *Config) SetProjectAllowlist(name string, cidrs []string) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].Allowlist = append([]string(nil), cidrs...)
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c *Config) SetProjectExposure(name string, until *string) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].ExposedUntil = until
			return c.Normalize()
		}
	}
	return fmt.Errorf("projeto não encontrado: %s", normalizedName)
}

func (c *Config) SetGlobalAuth(users []AuthUser) error {
	c.AuthUsers = append([]AuthUser(nil), users...)
	return c.Normalize()
}

func (c *Config) SetProjectAuth(name string, enabled *bool, users []AuthUser) error {
	normalizedName, err := NormalizeName(name)
	if err != nil {
		return err
	}
	for i := range c.Projects {
		if c.Projects[i].Name == normalizedName {
			c.Projects[i].AuthEnabled = enabled
			if users != nil {
				c.Projects[i].AuthUsers = append([]AuthUser(nil), users...)
			}
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

func (c Config) Validate() error {
	copy := c
	return copy.Normalize()
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
