package api

import (
	"context"

	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/domain"
)

// The Server methods below are the transport adapter's application boundary.
// They keep the command/query services private while allowing the optional
// Wails shell to use the same instances as the HTTP adapter.
func (s *Server) LinkProject(ctx context.Context, name, path string) (domain.Project, application.ApplyResult, error) {
	return s.commands.LinkProject(ctx, application.LinkProjectCommand{Name: name, Path: path})
}

func (s *Server) UnlinkProject(ctx context.Context, name string) (domain.Project, application.ApplyResult, error) {
	return s.commands.UnlinkProject(ctx, application.UnlinkProjectCommand{Name: name})
}

func (s *Server) ParkDirectory(ctx context.Context, path string) (domain.Park, application.ApplyResult, error) {
	return s.commands.ParkDirectory(ctx, application.ParkDirectoryCommand{Path: path})
}

func (s *Server) UnparkDirectory(ctx context.Context, path string) (domain.Park, application.ApplyResult, error) {
	return s.commands.UnparkDirectory(ctx, application.UnparkDirectoryCommand{Path: path})
}

func (s *Server) IgnoreProject(ctx context.Context, selector string) (application.ApplyResult, error) {
	return s.commands.IgnoreProject(ctx, application.IgnoreProjectCommand{Selector: selector})
}

func (s *Server) UnignoreProject(ctx context.Context, path string) (application.ApplyResult, error) {
	return s.commands.UnignoreProject(ctx, application.UnignoreProjectCommand{Path: path})
}

func (s *Server) SetDefaultMode(ctx context.Context, mode domain.Mode) (application.ApplyResult, error) {
	return s.commands.SetDefaultMode(ctx, application.SetDefaultModeCommand{Mode: mode})
}

func (s *Server) SetProjectMode(ctx context.Context, name string, mode *domain.Mode) (application.ApplyResult, error) {
	return s.commands.SetProjectMode(ctx, application.SetProjectModeCommand{Name: name, Mode: mode})
}

func (s *Server) SaveGlobalSettings(ctx context.Context, settings application.GlobalSettings) (application.ApplyResult, error) {
	return s.commands.SaveGlobalSettings(ctx, settings)
}

func (s *Server) QueryConfig(ctx context.Context) (domain.Config, error) {
	return s.queries.Config(ctx)
}

func (s *Server) QueryEffectiveConfig(ctx context.Context, cfg domain.Config) (domain.Config, error) {
	return s.queries.EffectiveConfig(ctx, cfg)
}

func (s *Server) QueryRevision(ctx context.Context) uint64 {
	return s.queries.Revision(ctx)
}
