package domain

// Network exposure and route allocation rules belong to this aggregate slice.

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

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
