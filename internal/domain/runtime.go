package domain

// Runtime project preferences are kept separate from project registration.

import (
	"fmt"
	pathpkg "path"
	"strings"
	"time"
)

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
