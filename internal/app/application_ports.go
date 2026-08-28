package app

import (
	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

func (a *App) resourceUseCases() *application.ResourceUseCases {
	if a == nil {
		return application.NewResourceUseCases(application.ResourceDependencies{})
	}
	edge := a.edgeCaddy()
	return application.NewResourceUseCases(application.ResourceDependencies{
		Store:             a.Store,
		Caddy:             edge,
		CaddyLifecycle:    edge,
		CaddyCertificates: edge,
		Firewall:          platform.NewApplicationFirewall(a.Firewall),
		TrustStore:        platform.WindowsTrustStore{},
		Network:           platform.HostNetwork{Listening: a.ExternalListeners},
		Clock:             platform.SystemClock{NowFunc: a.Now},
	})
}
