package domain

// PHP and Composer are the runtime configuration domain models.

import (
	"fmt"
	pathpkg "path"
	"regexp"
	"sort"
	"strings"
	"time"
)

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
