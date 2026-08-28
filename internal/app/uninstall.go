package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/config"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

func (a *App) ensureInstallationManifest(ctx context.Context) error {
	resources := a.managedInstallResources()
	if os.Getenv("DEVLAN_TEST_MOCK") != "1" && a.WSL.Distribution != "" {
		if output, err := a.WSL.RunOperation(ctx, platform.WSLOperationInstall, "/bin/cat", "/etc/devlan/bootstrap-packages"); err == nil {
			seen := make(map[string]struct{})
			for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
				packageName := strings.TrimSpace(line)
				if packageName == "" || strings.ContainsAny(packageName, "\\r\\n\\t") || !validPackageName(packageName) {
					continue
				}
				if _, exists := seen[packageName]; exists {
					continue
				}
				seen[packageName] = struct{}{}
				resources = append(resources, config.ManifestResource{ID: "wsl.package." + packageName, Scope: "wsl", Kind: "package", Package: packageName, Remove: true, Ownership: config.OwnershipCreated, Distribution: a.WSL.Distribution})
			}
		}
		if output, err := a.WSL.RunOperation(ctx, platform.WSLOperationInstall, "/bin/cat", "/etc/devlan/bootstrap-files"); err == nil {
			seen := make(map[string]struct{})
			for _, resource := range resources {
				seen[resource.ID] = struct{}{}
			}
			for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
				path := strings.TrimSpace(line)
				if path == "" || !safeWSLManagedPath(path) {
					continue
				}
				id := "wsl.bootstrap-file." + strings.NewReplacer("/", "-", ".", "_").Replace(strings.TrimPrefix(path, "/"))
				if _, exists := seen[id]; exists {
					continue
				}
				seen[id] = struct{}{}
				resources = append(resources, config.ManifestResource{ID: id, Scope: "wsl", Kind: "file", Path: path, Remove: true, Ownership: config.OwnershipCreated, Distribution: a.WSL.Distribution})
			}
		}
		if output, err := a.WSL.RunOperation(ctx, platform.WSLOperationInstall, "/bin/cat", "/etc/devlan/php-pool.path"); err == nil {
			poolPath := strings.TrimSpace(output)
			if safeWSLManagedPath(poolPath) && strings.HasSuffix(poolPath, "/fpm/pool.d/www.conf") {
				found := false
				for _, resource := range resources {
					if resource.ID == "wsl.php-fpm-pool" {
						found = true
						break
					}
				}
				if !found {
					resources = append(resources, config.ManifestResource{
						ID: "wsl.php-fpm-pool", Scope: "wsl", Kind: "file", Path: poolPath,
						Target: "/etc/devlan/php-pool.before", Restore: true,
						Ownership: config.OwnershipModified, Distribution: a.WSL.Distribution,
					})
				}
			}
		}
		if output, err := a.WSL.RunOperation(ctx, platform.WSLOperationInstall, "/bin/sh", "-c", `if [ -f /etc/devlan/wsl.conf.before ]; then printf modified; elif [ -e /etc/devlan/wsl.conf.missing ]; then printf created; else printf unknown; fi`); err == nil {
			for index := range resources {
				if resources[index].ID != "wsl.systemd-config" {
					continue
				}
				switch strings.TrimSpace(output) {
				case "modified":
					resources[index].Ownership = config.OwnershipModified
				case "created":
					resources[index].Ownership = config.OwnershipCreated
				}
			}
		}
		if output, err := a.WSL.RunOperation(ctx, platform.WSLOperationInstall, "/bin/sh", "-c", `if [ -f /etc/devlan/caddyfile.before ]; then printf modified; elif [ -e /etc/devlan/caddyfile.missing ]; then printf created; else printf unknown; fi`); err == nil {
			for index := range resources {
				if resources[index].ID != "wsl.caddy-config" {
					continue
				}
				switch strings.TrimSpace(output) {
				case "modified":
					resources[index].Ownership = config.OwnershipModified
				case "created":
					resources[index].Ownership = config.OwnershipCreated
				}
			}
		}
		if output, err := a.WSL.RunOperation(ctx, platform.WSLOperationInstall, "/bin/sh", "-c", `if [ -e /etc/devlan/caddy-service.before ]; then printf preexisting; elif [ -e /etc/devlan/caddy-service.missing ]; then printf created; else printf unknown; fi`); err == nil {
			for index := range resources {
				if resources[index].ID != "wsl.caddy-service" {
					continue
				}
				switch strings.TrimSpace(output) {
				case "preexisting":
					resources[index].Ownership = config.OwnershipPreexisting
				case "created":
					resources[index].Ownership = config.OwnershipCreated
				}
			}
		}
		if output, err := a.WSL.RunOperation(ctx, platform.WSLOperationInstall, "/bin/sh", "-c", `if [ -e /etc/devlan/caddy-data.before ]; then printf preexisting; elif [ -e /etc/devlan/caddy-data.missing ]; then printf created; else printf unknown; fi`); err == nil {
			for index := range resources {
				if resources[index].ID != "wsl.caddy-data" {
					continue
				}
				switch strings.TrimSpace(output) {
				case "preexisting":
					resources[index].Ownership = config.OwnershipPreexisting
				case "created":
					resources[index].Ownership = config.OwnershipCreated
				}
			}
		}
	}
	if _, err := a.Store.EnsureInstallManifest(resources, a.WSL.Distribution); err != nil {
		return err
	}
	return nil
}

// refreshWSLManagedFingerprints is called only after an operation that is
// known to publish a WSL file. Keeping it separate from manifest discovery
// prevents a user's later edit from being mistaken for DevLAN's applied state.
func (a *App) refreshWSLManagedFingerprints(ctx context.Context, ids ...string) error {
	if os.Getenv("DEVLAN_TEST_MOCK") == "1" || a.WSL.Distribution == "" {
		return nil
	}
	for _, id := range ids {
		manifest, present, err := a.Store.LoadManifest()
		if err != nil {
			return err
		}
		if !present {
			return nil
		}
		var path string
		for _, resource := range manifest.Resources {
			if resource.ID == id {
				path = resource.Path
				break
			}
		}
		if path == "" || !safeWSLManagedPath(path) {
			continue
		}
		hash, err := a.wslFileSHA256(ctx, path)
		if err != nil || hash == "" {
			continue
		}
		if err := a.Store.UpdateManifestResource(id, func(resource *config.ManifestResource) {
			resource.ManagedSHA256 = hash
		}); err != nil {
			return err
		}
	}
	return nil
}

func validPackageName(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune(".+-:_", r) {
			continue
		}
		return false
	}
	return true
}

func (a *App) wslFileSHA256(ctx context.Context, path string) (string, error) {
	if a.WSL.Distribution == "" || !safeWSLManagedPath(path) {
		return "", errors.New("caminho WSL não identificável")
	}
	output, err := a.WSL.RunOperation(ctx, platform.WSLOperationInstall, "/usr/bin/sha256sum", "--", path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(output)
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", errors.New("fingerprint SHA-256 WSL inválido")
	}
	for _, r := range fields[0] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return "", errors.New("fingerprint SHA-256 WSL inválido")
		}
	}
	return strings.ToLower(fields[0]), nil
}

func (a *App) wslResourceChangedSinceApply(ctx context.Context, resource config.ManifestResource) bool {
	if resource.ManagedSHA256 == "" || resource.Path == "" || os.Getenv("DEVLAN_TEST_MOCK") == "1" {
		return false
	}
	current, err := a.wslFileSHA256(ctx, resource.Path)
	return err == nil && current != resource.ManagedSHA256
}

// managedInstallResources is deliberately a closed list. New resources must
// be added here and to the manifest contract; uninstall never recursively
// searches a user's home directory or a project for files to delete.
func (a *App) managedInstallResources() []config.ManifestResource {
	paths := a.Store.Paths()
	resources := []config.ManifestResource{
		{ID: "data.cli-binary", Scope: "devlan", Kind: "file", Path: paths.Binary, Remove: true},
		{ID: "data.cli-bin", Scope: "devlan", Kind: "directory", Path: paths.BinDir, Remove: true},
		{ID: "data.distribution", Scope: "devlan", Kind: "file", Path: paths.Distribution, Remove: true},
		{ID: "data.config", Scope: "devlan", Kind: "file", Path: paths.Config, Remove: true},
		{ID: "data.state", Scope: "devlan", Kind: "file", Path: paths.State, Remove: true},
		{ID: "data.api-token", Scope: "devlan", Kind: "file", Path: paths.APIToken, Remove: true},
		{ID: "data.api-endpoint", Scope: "devlan", Kind: "file", Path: paths.APIEndpoint, Remove: true},
		{ID: "data.telemetry", Scope: "devlan", Kind: "file", Path: paths.Telemetry, Remove: true},
		{ID: "data.telemetry-queue", Scope: "devlan", Kind: "file", Path: paths.TelemetryQueue, Remove: true},
		{ID: "data.ca-export", Scope: "devlan", Kind: "file", Path: paths.CARootExport, Remove: true},
		{ID: "data.manifest", Scope: "devlan", Kind: "file", Path: paths.Manifest, Remove: true},
		{ID: "data.install-manifest", Scope: "devlan", Kind: "file", Path: paths.InstallManifest, Remove: true},
		{ID: "data.journal", Scope: "devlan", Kind: "file", Path: paths.Journal, Remove: true},
		{ID: "data.previous-config", Scope: "devlan", Kind: "file", Path: paths.PreviousConfig, Remove: true},
		{ID: "data.previous-state", Scope: "devlan", Kind: "file", Path: paths.PreviousState, Remove: true},
		{ID: "data.generated", Scope: "devlan", Kind: "directory", Path: paths.GeneratedDir, Remove: true},
		{ID: "data.logs", Scope: "devlan", Kind: "directory", Path: paths.LogsDir, Remove: true},
		{ID: "data.backups", Scope: "devlan", Kind: "directory", Path: paths.BackupsDir, Remove: true},
		{ID: "windows.wslconfig", Scope: "shared", Kind: "file", Path: a.WSLConfigPath, Restore: true},
		{ID: "windows.firewall", Scope: "windows", Kind: "firewall", Target: platform.FirewallRuleName, Remove: true, Ownership: config.OwnershipCreated},
		{ID: "hyperv.firewall", Scope: "windows", Kind: "firewall", Target: "DevLAN-HyperV", Remove: true, Ownership: config.OwnershipCreated},
		// Trust is optional. Until Trust records a certificate thumbprint, treat
		// this entry as pre-existing so a normal uninstall does not report a
		// conflict for a certificate that was never installed by DevLAN.
		{ID: "windows.ca-trust", Scope: "windows", Kind: "trust", Remove: true, Ownership: config.OwnershipPreexisting},
	}
	// Never target the default WSL distribution implicitly. The installer writes
	// wsl-distribution before registering these resources; without that identity
	// all WSL artifacts are omitted from a legacy/partial uninstall plan.
	if strings.TrimSpace(a.WSL.Distribution) != "" {
		resources = append(resources,
			config.ManifestResource{ID: "wsl.devlan-client", Scope: "wsl", Kind: "file", Path: "/usr/local/bin/devlan", Remove: true, Ownership: config.OwnershipCreated, Distribution: a.WSL.Distribution},
			config.ManifestResource{ID: "wsl.devlan-config", Scope: "wsl", Kind: "directory", Path: "/etc/devlan", Remove: true, Ownership: config.OwnershipCreated, Distribution: a.WSL.Distribution},
			config.ManifestResource{ID: "wsl.caddy-config", Scope: "wsl", Kind: "file", Path: "/etc/caddy/Caddyfile", Remove: true, Ownership: config.OwnershipPreexisting, Distribution: a.WSL.Distribution},
			config.ManifestResource{ID: "wsl.caddy-service", Scope: "wsl", Kind: "service", Target: "caddy", Remove: true, Ownership: config.OwnershipPreexisting, Distribution: a.WSL.Distribution},
			config.ManifestResource{ID: "wsl.caddy-data", Scope: "wsl", Kind: "directory", Path: "/var/lib/caddy", Remove: true, Ownership: config.OwnershipPreexisting, Distribution: a.WSL.Distribution},
			config.ManifestResource{ID: "wsl.systemd-config", Scope: "wsl", Kind: "file", Path: "/etc/wsl.conf", Restore: true, Ownership: config.OwnershipPreexisting, Distribution: a.WSL.Distribution},
		)
	}
	if _, err := os.Stat(paths.ToolchainMarker); err == nil {
		resources = append(resources, config.ManifestResource{
			ID: "data.toolchains", Scope: "devlan", Kind: "directory", Path: paths.ToolchainsDir, Remove: true, Ownership: config.OwnershipCreated,
		})
	}
	return resources
}

func mergeManifestResources(defaults []config.ManifestResource, manifest config.InstallManifest, present bool) []config.ManifestResource {
	byID := make(map[string]config.ManifestResource, len(defaults)+len(manifest.Resources))
	order := make([]string, 0, len(defaults)+len(manifest.Resources))
	for _, resource := range defaults {
		byID[resource.ID] = resource
		order = append(order, resource.ID)
	}
	if present {
		for _, resource := range manifest.Resources {
			if _, exists := byID[resource.ID]; !exists {
				order = append(order, resource.ID)
			}
			byID[resource.ID] = resource
		}
	}
	resources := make([]config.ManifestResource, 0, len(order))
	for _, id := range order {
		resources = append(resources, byID[id])
	}
	return resources
}

func (a *App) PlanUninstall(ctx context.Context, options UninstallOptions) (UninstallPlan, error) {
	if err := options.Validate(); err != nil {
		return UninstallPlan{}, err
	}
	manifest, present, err := a.Store.LoadManifest()
	if err != nil {
		return UninstallPlan{}, err
	}
	plan := UninstallPlan{
		Version:          config.InstallManifestVersion,
		DataDir:          a.Store.Dir,
		Manifest:         present,
		Legacy:           !present,
		KeepData:         options.KeepData,
		KeepDependencies: options.KeepDependencies,
		Purge:            options.Purge,
		Items:            make([]UninstallItem, 0),
	}
	if cfg, loadErr := a.Store.Load(); loadErr == nil {
		plan.ProjectCount = len(cfg.Projects)
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		plan.Warnings = append(plan.Warnings, "não foi possível ler a configuração para contar projetos: "+loadErr.Error())
	}
	if plan.Legacy {
		plan.Warnings = append(plan.Warnings, "instalação sem manifesto: recursos externos sem proveniência serão preservados")
	}

	resources := mergeManifestResources(a.managedInstallResources(), manifest, present)
	for _, resource := range resources {
		item := a.planResource(ctx, resource, options)
		if item.Target == "" {
			item.Target = resource.Path
		}
		if item.Target == "" {
			item.Target = resource.ID
		}
		plan.Items = append(plan.Items, item)
	}
	sort.SliceStable(plan.Items, func(i, j int) bool { return plan.Items[i].ID < plan.Items[j].ID })
	for _, item := range plan.Items {
		if item.Action == UninstallConflict {
			plan.Warnings = append(plan.Warnings, item.ID+": "+item.Detail)
		}
		if item.ID == "windows.wslconfig" && item.Action == UninstallRestore {
			plan.Pending = true
			plan.Warnings = append(plan.Warnings, "a restauração de .wslconfig exige `wsl --shutdown` para entrar em vigor")
		}
		if item.ID == "wsl.systemd-config" && item.Action == UninstallRestore {
			plan.Pending = true
			plan.Warnings = append(plan.Warnings, "a restauração de /etc/wsl.conf exige reiniciar a distribuição WSL")
		}
	}
	return plan, nil
}

func (a *App) planResource(ctx context.Context, resource config.ManifestResource, options UninstallOptions) UninstallItem {
	item := UninstallItem{ID: resource.ID, Scope: resource.Scope, Kind: resource.Kind, Target: resource.Target, Distribution: resource.Distribution}
	if item.Distribution == "" {
		item.Distribution = a.WSL.Distribution
	}
	if resource.Scope == "wsl" {
		if strings.TrimSpace(a.WSL.Distribution) == "" {
			item.Action, item.Detail = UninstallConflict, "distribuição WSL não identificada; recurso preservado"
			return item
		}
		if resource.Distribution != "" && resource.Distribution != a.WSL.Distribution {
			item.Action, item.Detail = UninstallConflict, "recurso pertence a outra distribuição WSL"
			return item
		}
	}
	if resource.Scope == "devlan" {
		if options.KeepData {
			item.Action, item.Detail = UninstallPreserve, "preservado por --keep-data"
		} else {
			item.Action, item.Detail = UninstallRemove, "arquivo gerenciado do DevLAN"
		}
		return item
	}
	if resource.Kind == "package" && options.KeepDependencies {
		item.Action, item.Detail = UninstallPreserve, "dependência preservada por --keep-dependencies"
		return item
	}
	if resource.ID == "wsl.systemd-config" {
		switch resource.Ownership {
		case config.OwnershipModified:
			if a.wslResourceChangedSinceApply(ctx, resource) {
				item.Action, item.Detail = UninstallConflict, "arquivo WSL foi alterado depois da aplicação do DevLAN"
				return item
			}
			item.Action, item.Detail = UninstallRestore, "restaurar backup WSL salvo pelo bootstrap"
			return item
		case config.OwnershipCreated:
			if a.wslResourceChangedSinceApply(ctx, resource) {
				item.Action, item.Detail = UninstallConflict, "arquivo WSL criado pelo DevLAN foi alterado depois da aplicação"
				return item
			}
			item.Action, item.Detail = UninstallRemove, "arquivo WSL criado pelo bootstrap"
			return item
		}
	}
	if resource.ID == "wsl.caddy-config" {
		switch resource.Ownership {
		case config.OwnershipModified:
			if a.wslResourceChangedSinceApply(ctx, resource) {
				item.Action, item.Detail = UninstallConflict, "Caddyfile WSL foi alterado depois da aplicação do DevLAN"
				return item
			}
			item.Action, item.Detail = UninstallRestore, "restaurar Caddyfile anterior salvo no WSL"
			return item
		case config.OwnershipCreated:
			if a.wslResourceChangedSinceApply(ctx, resource) {
				item.Action, item.Detail = UninstallConflict, "Caddyfile WSL criado pelo DevLAN foi alterado depois da aplicação"
				return item
			}
			item.Action, item.Detail = UninstallRemove, "Caddyfile criado pelo DevLAN"
			return item
		}
	}
	if resource.ID == "wsl.php-fpm-pool" && resource.Ownership == config.OwnershipModified {
		if a.wslResourceChangedSinceApply(ctx, resource) {
			item.Action, item.Detail = UninstallConflict, "pool PHP WSL foi alterado depois da aplicação do DevLAN"
			return item
		}
		item.Action, item.Detail = UninstallRestore, "restaurar pool PHP anterior salvo pelo bootstrap"
		return item
	}
	// Shared configuration and a potentially pre-existing Caddyfile are never
	// deleted by an undifferentiated purge. A resource that was explicitly
	// recorded as created/modified by DevLAN is safe to remove or restore.
	if resource.Scope == "shared" {
		if resource.Restore && resource.Ownership == config.OwnershipCreated {
			item.Action, item.Detail = UninstallRemove, "arquivo compartilhado criado pelo DevLAN"
			return item
		}
		if resource.Restore && resource.Ownership == config.OwnershipModified {
			if resource.BackupPath == "" || resource.BeforeSHA256 == "" {
				item.Action, item.Detail = UninstallConflict, "não há snapshot anterior para restauração"
				return item
			}
			current, exists := config.FileSHA256(resource.Path)
			if exists && resource.ManagedSHA256 != "" && current != resource.ManagedSHA256 {
				item.Action, item.Detail = UninstallConflict, "arquivo compartilhado foi alterado depois da aplicação do DevLAN"
				return item
			}
			item.Action, item.Detail = UninstallRestore, "restaurar snapshot anterior do arquivo compartilhado"
			return item
		}
		item.Action, item.Detail = UninstallConflict, "configuração compartilhada sem proveniência comprovada"
		return item
	}
	if resource.ID == "wsl.systemd-config" || resource.ID == "wsl.caddy-config" {
		item.Action, item.Detail = UninstallConflict, "configuração compartilhada sem proveniência comprovada"
		return item
	}
	switch resource.Ownership {
	case config.OwnershipPreexisting, config.OwnershipAdopted:
		item.Action, item.Detail = UninstallPreserve, "recurso preexistente/adotado"
		return item
	case config.OwnershipUnknown, "":
		if options.Purge && options.Yes {
			item.Action, item.Detail = UninstallRemove, "recurso legado selecionado por --purge"
		} else {
			item.Action, item.Detail = UninstallConflict, "proveniência não comprovada; preservado por segurança"
		}
		return item
	}
	if resource.Restore {
		if resource.BackupPath == "" || resource.BeforeSHA256 == "" {
			if resource.ManagedSHA256 == "" {
				item.Action, item.Detail = UninstallConflict, "não há snapshot anterior para restauração"
				return item
			}
			item.Action, item.Detail = UninstallRemove, "arquivo criado pelo DevLAN"
			return item
		}
		current, exists := config.FileSHA256(resource.Path)
		if exists && resource.ManagedSHA256 != "" && current != resource.ManagedSHA256 {
			item.Action, item.Detail = UninstallConflict, "arquivo foi alterado depois da aplicação do DevLAN"
			return item
		}
		item.Action, item.Detail = UninstallRestore, "restaurar snapshot anterior"
		return item
	}
	item.Action, item.Detail = UninstallRemove, "recurso criado pelo DevLAN"
	return item
}

// Uninstall is kept as the compatibility entry point used by the API and old
// callers. New callers should use UninstallWithOptions to inspect the plan.
func (a *App) Uninstall(ctx context.Context) (ApplyResult, error) {
	result, err := a.UninstallWithOptions(ctx, UninstallOptions{})
	return result.ApplyResult, err
}

func (a *App) UninstallWithOptions(ctx context.Context, options UninstallOptions) (UninstallResult, error) {
	if err := options.Validate(); err != nil {
		return UninstallResult{}, err
	}
	plan, err := a.PlanUninstall(ctx, options)
	if err != nil {
		return UninstallResult{}, err
	}
	result := UninstallResult{
		ApplyResult: ApplyResult{Warnings: append([]string(nil), plan.Warnings...)},
		Plan:        plan,
		Completed:   false,
	}
	if options.DryRun {
		result.Completed = !planHasAction(plan, UninstallConflict)
		return result, nil
	}
	_ = a.appendLog("uninstall iniciado")

	// Process/service and firewall teardown are intentionally performed before
	// removing the generated files they consume.
	if caddyClient := a.edgeCaddy(); caddyClient.Runner != nil {
		if stopErr := caddyClient.Stop(ctx); stopErr != nil && runtime.GOOS == "windows" {
			result.Warnings = append(result.Warnings, "não foi possível parar o serviço Caddy WSL único: "+stopErr.Error())
		}
	}
	if err := a.removeFirewall(ctx); err != nil && runtime.GOOS == "windows" {
		result.Warnings = append(result.Warnings, "não foi possível remover a regra de firewall DevLAN; execute uninstall como administrador")
	}

	deferredCleanupScheduled := false
	resources := mergeManifestResources(a.managedInstallResources(), mustLoadManifest(a.Store), plan.Manifest)
	sort.SliceStable(resources, func(left, right int) bool {
		return uninstallResourcePriority(resources[left]) < uninstallResourcePriority(resources[right])
	})
	for _, resource := range resources {
		item := findUninstallItem(plan, resource.ID)
		if item == nil || item.Action != UninstallRemove && item.Action != UninstallRestore {
			continue
		}
		if resource.Kind == "path" {
			if runtime.GOOS == "windows" && item.Action == UninstallRemove {
				target := resource.Target
				if target == "" {
					target = filepath.Dir(filepath.Join(a.Store.Dir, "bin", "devlan.exe"))
				}
				if removeErr := platform.RemoveUserPathEntry(target); removeErr != nil {
					result.Warnings = append(result.Warnings, removeErr.Error())
				}
			}
			continue
		}
		if resource.Kind == "service" {
			if resource.Scope == "wsl" && item.Action == UninstallRemove {
				if removeErr := a.removeWSLResource(ctx, resource); removeErr != nil {
					result.Warnings = append(result.Warnings, removeErr.Error())
				}
			}
			continue
		}
		if resource.Scope == "devlan" {
			if item.Action == UninstallRemove {
				switch resource.ID {
				case "data.cli-binary", "data.cli-bin":
					if runtime.GOOS == "windows" {
						if !deferredCleanupScheduled {
							targets := []string{a.Store.Paths().Binary, a.Store.Paths().BinDir}
							if executable, executableErr := os.Executable(); executableErr == nil && strings.TrimSpace(executable) != "" {
								targets = append(targets, executable)
							}
							if scheduleErr := platform.ScheduleDeferredRemoval(targets...); scheduleErr != nil {
								result.Warnings = append(result.Warnings, "remoção do executável pendente: "+scheduleErr.Error())
							} else {
								deferredCleanupScheduled = true
							}
						}
					} else if removeErr := removeHostResource(resource); removeErr != nil {
						result.Warnings = append(result.Warnings, removeErr.Error())
					}
				case "data.toolchains":
					if removeErr := removeHostResource(resource); removeErr != nil {
						result.Warnings = append(result.Warnings, removeErr.Error())
					}
				}
			}
			continue
		}
		if resource.Kind == "firewall" {
			continue
		}
		if resource.Kind == "trust" {
			if runtime.GOOS == "windows" && resource.Fingerprint != "" && item.Action == UninstallRemove {
				if removeErr := platform.RemoveCARoot(ctx, resource.Fingerprint); removeErr != nil {
					result.Warnings = append(result.Warnings, removeErr.Error())
				}
			}
			continue
		}
		if resource.Scope == "shared" && item.Action == UninstallRestore {
			if restoreErr := restoreManifestResource(resource); restoreErr != nil {
				result.Warnings = append(result.Warnings, restoreErr.Error())
			}
			continue
		}
		if resource.Scope == "shared" && item.Action == UninstallRemove {
			if removeErr := removeHostResource(resource); removeErr != nil {
				result.Warnings = append(result.Warnings, removeErr.Error())
			}
			continue
		}
		if resource.Scope == "wsl" {
			if resource.ID == "wsl.php-fpm-pool" && item.Action == UninstallRestore {
				if restoreErr := a.restoreWSLPHPFPMConfig(ctx, resource); restoreErr != nil {
					result.Warnings = append(result.Warnings, restoreErr.Error())
				}
				continue
			}
			if resource.ID == "wsl.systemd-config" && item.Action == UninstallRestore {
				if restoreErr := a.restoreWSLSystemdConfig(ctx, resource); restoreErr != nil {
					result.Warnings = append(result.Warnings, restoreErr.Error())
				}
				continue
			}
			if resource.ID == "wsl.caddy-config" && item.Action == UninstallRestore {
				if restoreErr := a.restoreWSLCaddyConfig(ctx, resource); restoreErr != nil {
					result.Warnings = append(result.Warnings, restoreErr.Error())
				}
				continue
			}
			if removeErr := a.removeWSLResource(ctx, resource); removeErr != nil {
				result.Warnings = append(result.Warnings, removeErr.Error())
			}
		}
	}
	if err := a.Store.RemoveManagedFilesWithOptions(options.KeepData); err != nil {
		return result, err
	}
	result.Completed = !planHasAction(plan, UninstallConflict) && len(result.Warnings) == 0
	if planHasAction(plan, UninstallConflict) {
		result.Warnings = append(result.Warnings, "desinstalação parcial: recursos em conflito foram preservados")
	}
	return result, nil
}

func (a *App) restoreWSLSystemdConfig(ctx context.Context, resource config.ManifestResource) error {
	if resource.Distribution != "" && resource.Distribution != a.WSL.Distribution {
		return fmt.Errorf("restaurar %s: distribuição WSL incompatível", resource.ID)
	}
	// All paths are fixed literals. The shell only selects between the two
	// bootstrap markers and never interpolates user/project input.
	const script = `set -eu
if [ -f /etc/devlan/wsl.conf.before ]; then
    /bin/cp -- /etc/devlan/wsl.conf.before /etc/wsl.conf
elif [ -e /etc/devlan/wsl.conf.missing ]; then
    /bin/rm -f -- /etc/wsl.conf
else
    exit 17
fi`
	if _, err := a.WSL.RunAsRootOperation(ctx, platform.WSLOperationInstall, "/bin/sh", "-c", script); err != nil {
		return fmt.Errorf("restaurar /etc/wsl.conf: %w", err)
	}
	return nil
}

func (a *App) restoreWSLCaddyConfig(ctx context.Context, resource config.ManifestResource) error {
	if resource.Distribution != "" && resource.Distribution != a.WSL.Distribution {
		return fmt.Errorf("restaurar %s: distribuição WSL incompatível", resource.ID)
	}
	const script = `set -eu
if [ -f /etc/devlan/caddyfile.before ]; then
    /bin/mkdir -p -- /etc/caddy
    /bin/cp -- /etc/devlan/caddyfile.before /etc/caddy/Caddyfile
elif [ -e /etc/devlan/caddyfile.missing ]; then
    /bin/rm -f -- /etc/caddy/Caddyfile
else
    exit 17
fi`
	if _, err := a.WSL.RunAsRootOperation(ctx, platform.WSLOperationInstall, "/bin/sh", "-c", script); err != nil {
		return fmt.Errorf("restaurar /etc/caddy/Caddyfile: %w", err)
	}
	return nil
}

func (a *App) restoreWSLPHPFPMConfig(ctx context.Context, resource config.ManifestResource) error {
	if resource.Distribution != "" && resource.Distribution != a.WSL.Distribution {
		return fmt.Errorf("restaurar %s: distribuição WSL incompatível", resource.ID)
	}
	if !safeWSLManagedPath(resource.Path) {
		return fmt.Errorf("restaurar pool PHP fora da allowlist: %s", resource.Path)
	}
	const script = `set -eu
if [ -f /etc/devlan/php-pool.before ]; then
    /bin/cp -- /etc/devlan/php-pool.before "$1"
elif [ -e /etc/devlan/php-pool.missing ]; then
    /bin/rm -f -- "$1"
else
    exit 17
fi`
	if _, err := a.WSL.RunAsRootOperation(ctx, platform.WSLOperationInstall, "/bin/sh", "-c", script, "devlan", resource.Path); err != nil {
		return fmt.Errorf("restaurar pool PHP %s: %w", resource.Path, err)
	}
	return nil
}

func uninstallResourcePriority(resource config.ManifestResource) int {
	switch resource.ID {
	case "wsl.systemd-config", "wsl.caddy-config", "wsl.php-fpm-pool":
		// These resources use backups/markers stored in /etc/devlan.
		return 10
	case "wsl.devlan-config":
		// The marker directory is the final WSL artifact to remove.
		return 90
	default:
		return 50
	}
}

func planHasAction(plan UninstallPlan, action UninstallAction) bool {
	for _, item := range plan.Items {
		if item.Action == action {
			return true
		}
	}
	return false
}

func findUninstallItem(plan UninstallPlan, id string) *UninstallItem {
	for index := range plan.Items {
		if plan.Items[index].ID == id {
			return &plan.Items[index]
		}
	}
	return nil
}

func mustLoadManifest(store config.Store) config.InstallManifest {
	manifest, _, err := store.LoadManifest()
	if err != nil {
		return config.InstallManifest{}
	}
	return manifest
}

func (a *App) removeWSLResource(ctx context.Context, resource config.ManifestResource) error {
	if resource.Distribution != "" && resource.Distribution != a.WSL.Distribution {
		return fmt.Errorf("recurso WSL %s pertence à distribuição %q, não à selecionada %q", resource.ID, resource.Distribution, a.WSL.Distribution)
	}
	if resource.Kind == "package" {
		if strings.TrimSpace(resource.Package) == "" {
			return fmt.Errorf("pacote WSL %s sem nome exato no manifesto", resource.ID)
		}
		if _, err := a.WSL.RunAsRootOperation(ctx, platform.WSLOperationInstall, "apt-get", "purge", "-y", resource.Package); err != nil {
			return fmt.Errorf("remover pacote WSL %s: %w", resource.Package, err)
		}
		return nil
	}
	if resource.Kind == "service" {
		if _, err := a.WSL.RunAsRootOperation(ctx, platform.WSLOperationInstall, "systemctl", "disable", "--now", resource.Target); err != nil {
			return fmt.Errorf("desabilitar serviço WSL %s: %w", resource.Target, err)
		}
		return nil
	}
	if !safeWSLManagedPath(resource.Path) {
		return fmt.Errorf("caminho WSL fora da allowlist de uninstall: %s", resource.Path)
	}
	args := []string{"/bin/rm"}
	if resource.Kind == "directory" {
		args = append(args, "-rf")
	} else {
		args = append(args, "-f")
	}
	args = append(args, "--", resource.Path)
	if _, err := a.WSL.RunAsRootOperation(ctx, platform.WSLOperationInstall, args...); err != nil {
		return fmt.Errorf("remover recurso WSL %s: %w", resource.Path, err)
	}
	return nil
}

func safeWSLManagedPath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	for _, allowed := range []string{"/usr/local/bin/devlan", "/etc/devlan", "/etc/caddy/Caddyfile", "/var/lib/caddy", "/etc/wsl.conf", "/etc/apt/sources.list.d/caddy-stable.list", "/usr/share/keyrings/caddy-stable-archive-keyring.gpg"} {
		if clean == allowed {
			return true
		}
	}
	if strings.HasPrefix(clean, "/etc/apt/sources.list.d/") || clean == "/etc/apt/trusted.gpg.d/php.gpg" {
		return filepath.Base(clean) != "." && filepath.Base(clean) != ".."
	}
	if strings.HasPrefix(clean, "/etc/php/") && strings.HasSuffix(clean, "/fpm/pool.d/www.conf") {
		return !strings.Contains(clean, "..")
	}
	return false
}

func restoreManifestResource(resource config.ManifestResource) error {
	if resource.Path == "" || resource.BackupPath == "" {
		return fmt.Errorf("restaurar %s: snapshot ausente", resource.ID)
	}
	current, exists := config.FileSHA256(resource.Path)
	if exists && resource.ManagedSHA256 != "" && current != resource.ManagedSHA256 {
		return fmt.Errorf("restaurar %s: arquivo foi alterado depois da aplicação; preservado", resource.Path)
	}
	data, err := os.ReadFile(resource.BackupPath)
	if err != nil {
		return fmt.Errorf("ler backup de %s: %w", resource.Path, err)
	}
	dir := filepath.Dir(resource.Path)
	temporary, err := os.CreateTemp(dir, ".devlan-uninstall-*")
	if err != nil {
		return fmt.Errorf("preparar restauração de %s: %w", resource.Path, err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("escrever restauração de %s: %w", resource.Path, err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, resource.Path); err != nil {
		if removeErr := os.Remove(resource.Path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("publicar restauração de %s: %w", resource.Path, err)
		}
		if err := os.Rename(temporaryName, resource.Path); err != nil {
			return fmt.Errorf("publicar restauração de %s: %w", resource.Path, err)
		}
	}
	return nil
}

func removeHostResource(resource config.ManifestResource) error {
	if resource.Path == "" || resource.Path != filepath.Clean(resource.Path) {
		return fmt.Errorf("remover %s: caminho inválido", resource.ID)
	}
	if resource.Kind == "directory" {
		if err := os.RemoveAll(resource.Path); err != nil {
			return fmt.Errorf("remover %s: %w", resource.Path, err)
		}
		return nil
	}
	if err := os.Remove(resource.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remover %s: %w", resource.Path, err)
	}
	return nil
}
