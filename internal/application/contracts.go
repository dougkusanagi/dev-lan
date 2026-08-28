// Package application contains transport-neutral commands and queries.
// Concrete adapters are supplied by the composition root through small ports;
// the services keep those dependencies private so callers cannot reach around
// the use-case boundary.
package application

import (
	"context"
	"errors"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

var ErrUnavailable = errors.New("serviço de aplicação não configurado")

// ApplyResult is the transport-neutral result of a persisted mutation.
// app.App aliases this type while the application package owns the contract.
type ApplyResult struct {
	Warnings []string
	Status   string `json:"status,omitempty"`
	Revision uint64 `json:"revision,omitempty"`
}

// GlobalSettings is the validated intent used by the global-settings command.
// It deliberately contains no persistence or transport fields.
type GlobalSettings struct {
	DefaultMode       string
	WindowsPort       int
	HTTPSPort         int
	TLSEnabled        bool
	PHPDefaultVersion string
	Allowlist         []string
}

type LinkProjectCommand struct {
	Name string
	Path string
}

type UnlinkProjectCommand struct {
	Name string
}

type ParkDirectoryCommand struct {
	Path string
}

type UnparkDirectoryCommand struct {
	Path string
}

type IgnoreProjectCommand struct {
	Selector string
}

type UnignoreProjectCommand struct {
	Path string
}

type SetDefaultModeCommand struct {
	Mode domain.Mode
}

type SetProjectModeCommand struct {
	Name string
	Mode *domain.Mode
}

// ProjectCommandPort is the minimum mutation surface required by project
// commands. app.App satisfies it without the application package depending on
// the concrete app implementation.
type ProjectCommandPort interface {
	Link(context.Context, string, string) (domain.Project, ApplyResult, error)
	Unlink(context.Context, string) (domain.Project, ApplyResult, error)
	Park(context.Context, string) (domain.Park, ApplyResult, error)
	Unpark(context.Context, string) (domain.Park, ApplyResult, error)
	IgnoreProject(context.Context, string) (ApplyResult, error)
	UnignoreProject(context.Context, string) (ApplyResult, error)
	SetDefaultMode(context.Context, domain.Mode) (ApplyResult, error)
	SetProjectMode(context.Context, string, *domain.Mode) (ApplyResult, error)
}

type SettingsCommandPort interface {
	SaveGlobalSettings(context.Context, GlobalSettings) (ApplyResult, error)
}

// Commands exposes validated command DTOs while keeping the concrete
// mutation ports private to the application service.
type Commands struct {
	projects ProjectCommandPort
	settings SettingsCommandPort
}

func NewCommands(projects ProjectCommandPort, settings SettingsCommandPort) *Commands {
	return &Commands{projects: projects, settings: settings}
}

func (c *Commands) LinkProject(ctx context.Context, command LinkProjectCommand) (domain.Project, ApplyResult, error) {
	if c == nil || c.projects == nil {
		return domain.Project{}, ApplyResult{}, ErrUnavailable
	}
	return c.projects.Link(ctx, command.Name, command.Path)
}

func (c *Commands) UnlinkProject(ctx context.Context, command UnlinkProjectCommand) (domain.Project, ApplyResult, error) {
	if c == nil || c.projects == nil {
		return domain.Project{}, ApplyResult{}, ErrUnavailable
	}
	return c.projects.Unlink(ctx, command.Name)
}

func (c *Commands) ParkDirectory(ctx context.Context, command ParkDirectoryCommand) (domain.Park, ApplyResult, error) {
	if c == nil || c.projects == nil {
		return domain.Park{}, ApplyResult{}, ErrUnavailable
	}
	return c.projects.Park(ctx, command.Path)
}

func (c *Commands) UnparkDirectory(ctx context.Context, command UnparkDirectoryCommand) (domain.Park, ApplyResult, error) {
	if c == nil || c.projects == nil {
		return domain.Park{}, ApplyResult{}, ErrUnavailable
	}
	return c.projects.Unpark(ctx, command.Path)
}

func (c *Commands) IgnoreProject(ctx context.Context, command IgnoreProjectCommand) (ApplyResult, error) {
	if c == nil || c.projects == nil {
		return ApplyResult{}, ErrUnavailable
	}
	return c.projects.IgnoreProject(ctx, command.Selector)
}

func (c *Commands) UnignoreProject(ctx context.Context, command UnignoreProjectCommand) (ApplyResult, error) {
	if c == nil || c.projects == nil {
		return ApplyResult{}, ErrUnavailable
	}
	return c.projects.UnignoreProject(ctx, command.Path)
}

func (c *Commands) SetDefaultMode(ctx context.Context, command SetDefaultModeCommand) (ApplyResult, error) {
	if c == nil || c.projects == nil {
		return ApplyResult{}, ErrUnavailable
	}
	return c.projects.SetDefaultMode(ctx, command.Mode)
}

func (c *Commands) SetProjectMode(ctx context.Context, command SetProjectModeCommand) (ApplyResult, error) {
	if c == nil || c.projects == nil {
		return ApplyResult{}, ErrUnavailable
	}
	return c.projects.SetProjectMode(ctx, command.Name, command.Mode)
}

func (c *Commands) SaveGlobalSettings(ctx context.Context, settings GlobalSettings) (ApplyResult, error) {
	if c == nil || c.settings == nil {
		return ApplyResult{}, ErrUnavailable
	}
	return c.settings.SaveGlobalSettings(ctx, settings)
}

// QueryPort is the read surface needed by the critical configuration and
// effective-project queries. It is intentionally narrower than app.App.
type QueryPort interface {
	Config() (domain.Config, error)
	EffectiveConfig(context.Context, domain.Config) (domain.Config, error)
	Revision() uint64
}

type Queries struct {
	source QueryPort
}

func NewQueries(source QueryPort) *Queries {
	return &Queries{source: source}
}

func (q *Queries) Config(ctx context.Context) (domain.Config, error) {
	if q == nil || q.source == nil {
		return domain.Config{}, ErrUnavailable
	}
	if err := contextError(ctx); err != nil {
		return domain.Config{}, err
	}
	return q.source.Config()
}

func (q *Queries) EffectiveConfig(ctx context.Context, cfg domain.Config) (domain.Config, error) {
	if q == nil || q.source == nil {
		return domain.Config{}, ErrUnavailable
	}
	if err := contextError(ctx); err != nil {
		return domain.Config{}, err
	}
	return q.source.EffectiveConfig(ctx, cfg)
}

func (q *Queries) Revision(ctx context.Context) uint64 {
	if q == nil || q.source == nil || contextError(ctx) != nil {
		return 0
	}
	return q.source.Revision()
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
