package domain

// Authentication and exposure policy models and behavior.

import (
	"fmt"
	"strings"
	"time"
)

type AuthUser struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
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
