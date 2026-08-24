package domain

import (
	"errors"
	"fmt"
	pathpkg "path"
	"regexp"
	"sort"
	"strings"
	"time"
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
}

type Park struct {
	Path string `json:"path"`
	Mode *Mode  `json:"mode,omitempty"`
}

type Config struct {
	Version           int                `json:"version"`
	DefaultMode       Mode               `json:"default_mode"`
	LANAddress        string             `json:"lan_address"`
	WindowsPort       int                `json:"windows_port"`
	HTTPSPort         int                `json:"https_port"`
	TLSEnabled        bool               `json:"tls_enabled"`
	WSLPort           int                `json:"wsl_port"`
	PHPFPMOsocket     string             `json:"php_fpm_socket"`
	PHPDefaultVersion string             `json:"php_default_version"`
	PHPVersions       []PHPVersionConfig `json:"php_versions"`
	PHPFPMPool        PHPFPMPoolConfig   `json:"php_fpm_pool"`
	Composer          ComposerConfig     `json:"composer"`
	Projects          []Project          `json:"projects"`
	Parks             []Park             `json:"parks"`
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

func NewConfig() Config {
	return Config{
		Version:           1,
		DefaultMode:       ModePHP,
		LANAddress:        "auto",
		WindowsPort:       80,
		HTTPSPort:         443,
		WSLPort:           8181,
		PHPFPMOsocket:     "/run/php/php-fpm.sock",
		PHPDefaultVersion: "8.5",
		PHPFPMPool:        DefaultPHPFPMPoolConfig(),
		Composer:          ComposerConfig{Environment: ComposerPerVersion, Binary: "composer"},
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
