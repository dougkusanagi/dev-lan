package domain

// Configuration is the persisted domain aggregate and its invariants.

import (
	"fmt"
	pathpkg "path"
	"sort"
	"strings"
	"time"
)

const (
	ProtocolVersion = 1
	CoreVersion     = "0.0.1"
)

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

func (c Config) Validate() error {
	copy := c
	return copy.Normalize()
}
