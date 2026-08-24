package detect

import (
	"context"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/domain"
)

type JSResult struct {
	ProjectPath    string
	HasPackageJSON bool
	PackageManager string
	Framework      string
	DevScript      string
	BuildScript    string
	StaticDir      string
	HasStaticBuild bool
	IsSPA          bool
	HasDevServer   bool
}

type ProjectKind string

const (
	ProjectKindPHP    ProjectKind = "php"
	ProjectKindDev    ProjectKind = "dev"
	ProjectKindStatic ProjectKind = "static"
)

type DetectedProject struct {
	ProjectPath   string
	Kind          ProjectKind
	SuggestedMode domain.Mode
	PHP           PHPResult
	JS            JSResult
}

type packageJSON struct {
	Name            string            `json:"name"`
	PackageManager  string            `json:"packageManager"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// DetectJS analyzes the project directory for Node.js / JavaScript / static frameworks.
func (d Detector) DetectJS(ctx context.Context, projectPath string) (JSResult, error) {
	if d.Inspector == nil {
		return JSResult{}, fmt.Errorf("detector sem inspector configurado")
	}
	result := JSResult{
		ProjectPath:    projectPath,
		PackageManager: "npm",
		Framework:      "generic",
	}

	// Check package.json
	hasPkg, _ := d.Inspector.Exists(ctx, projectPath, "package.json")
	result.HasPackageJSON = hasPkg

	var pkg packageJSON
	if hasPkg {
		data, err := d.Inspector.ReadFile(ctx, projectPath, "package.json")
		if err == nil && len(data) > 0 {
			_ = json.Unmarshal(data, &pkg)
		}
	}

	// Detect Package Manager: 1) packageManager field, 2) lockfiles, 3) default npm
	if pkg.PackageManager != "" {
		pm := strings.ToLower(pkg.PackageManager)
		switch {
		case strings.HasPrefix(pm, "pnpm"):
			result.PackageManager = "pnpm"
		case strings.HasPrefix(pm, "yarn"):
			result.PackageManager = "yarn"
		case strings.HasPrefix(pm, "bun"):
			result.PackageManager = "bun"
		default:
			result.PackageManager = "npm"
		}
	} else {
		if exists, _ := d.Inspector.Exists(ctx, projectPath, "pnpm-lock.yaml"); exists {
			result.PackageManager = "pnpm"
		} else if exists, _ := d.Inspector.Exists(ctx, projectPath, "yarn.lock"); exists {
			result.PackageManager = "yarn"
		} else if exists, _ := d.Inspector.Exists(ctx, projectPath, "bun.lockb"); exists {
			result.PackageManager = "bun"
		} else if exists, _ := d.Inspector.Exists(ctx, projectPath, "bun.lock"); exists {
			result.PackageManager = "bun"
		} else if exists, _ := d.Inspector.Exists(ctx, projectPath, "package-lock.json"); exists {
			result.PackageManager = "npm"
		}
	}

	// Detect Framework
	deps := make(map[string]bool)
	for k := range pkg.Dependencies {
		deps[strings.ToLower(k)] = true
	}
	for k := range pkg.DevDependencies {
		deps[strings.ToLower(k)] = true
	}

	switch {
	case deps["next"] || fileAnyExists(ctx, d.Inspector, projectPath, "next.config.js", "next.config.mjs", "next.config.ts"):
		result.Framework = "next"
	case deps["nuxt"] || deps["nuxt3"] || fileAnyExists(ctx, d.Inspector, projectPath, "nuxt.config.ts", "nuxt.config.js"):
		result.Framework = "nuxt"
	case deps["astro"] || fileAnyExists(ctx, d.Inspector, projectPath, "astro.config.mjs", "astro.config.ts", "astro.config.js"):
		result.Framework = "astro"
	case deps["@sveltejs/kit"] || fileAnyExists(ctx, d.Inspector, projectPath, "svelte.config.js"):
		result.Framework = "sveltekit"
	case deps["vite"] || fileAnyExists(ctx, d.Inspector, projectPath, "vite.config.js", "vite.config.ts", "vite.config.mjs"):
		result.Framework = "vite"
	case deps["@remix-run/react"] || deps["@remix-run/dev"]:
		result.Framework = "remix"
	}

	// Detect Scripts
	if pkg.Scripts != nil {
		if _, ok := pkg.Scripts["dev"]; ok {
			result.HasDevServer = true
			result.DevScript = pmDevCommand(result.PackageManager, "dev")
		} else if _, ok := pkg.Scripts["start"]; ok {
			result.HasDevServer = true
			result.DevScript = pmDevCommand(result.PackageManager, "start")
		}
		if _, ok := pkg.Scripts["build"]; ok {
			result.BuildScript = pmBuildCommand(result.PackageManager, "build")
		}
	}

	// Detect Static build directory
	staticCandidates := []string{"dist", "build", "out", ".output/public", "public"}
	for _, candidate := range staticCandidates {
		if exists, _ := d.Inspector.Exists(ctx, projectPath, pathpkg.Join(candidate, "index.html")); exists {
			result.StaticDir = candidate
			result.HasStaticBuild = true
			result.IsSPA = true
			break
		}
		if isDir, _ := d.Inspector.Directory(ctx, pathpkg.Join(projectPath, candidate)); isDir {
			result.StaticDir = candidate
			result.HasStaticBuild = true
			break
		}
	}

	// Check root index.html
	if rootIndex, _ := d.Inspector.Exists(ctx, projectPath, "index.html"); rootIndex {
		result.IsSPA = true
		if result.StaticDir == "" {
			result.StaticDir = ""
			result.HasStaticBuild = true
		}
	}

	if !hasPkg && !result.HasStaticBuild && !result.IsSPA {
		return result, fmt.Errorf("projeto JavaScript ou estático não reconhecido")
	}

	return result, nil
}

func fileAnyExists(ctx context.Context, inspector Inspector, projectPath string, files ...string) bool {
	for _, f := range files {
		if exists, _ := inspector.Exists(ctx, projectPath, f); exists {
			return true
		}
	}
	return false
}

func pmDevCommand(pm, script string) string {
	switch pm {
	case "yarn":
		return "yarn " + script
	case "pnpm":
		return "pnpm run " + script
	case "bun":
		return "bun run " + script
	default:
		return "npm run " + script
	}
}

func pmBuildCommand(pm, script string) string {
	switch pm {
	case "yarn":
		return "yarn " + script
	case "pnpm":
		return "pnpm run " + script
	case "bun":
		return "bun run " + script
	default:
		return "npm run " + script
	}
}

// DetectProject automatically discovers whether the target is PHP, Dev (JS with dev script), or Static.
func (d Detector) DetectProject(ctx context.Context, projectPath string) (DetectedProject, error) {
	// First test for PHP
	if phpRes, err := d.DetectPHP(ctx, projectPath); err == nil {
		return DetectedProject{
			ProjectPath:   projectPath,
			Kind:          ProjectKindPHP,
			SuggestedMode: domain.ModePHP,
			PHP:           phpRes,
		}, nil
	}

	// Next test for JS / Static
	if jsRes, err := d.DetectJS(ctx, projectPath); err == nil {
		if jsRes.HasDevServer {
			return DetectedProject{
				ProjectPath:   projectPath,
				Kind:          ProjectKindDev,
				SuggestedMode: domain.ModeDev,
				JS:            jsRes,
			}, nil
		}
		return DetectedProject{
			ProjectPath:   projectPath,
			Kind:          ProjectKindStatic,
			SuggestedMode: domain.ModeStatic,
			JS:            jsRes,
		}, nil
	}

	return DetectedProject{ProjectPath: projectPath}, fmt.Errorf("tipo de projeto não identificado para: %s", projectPath)
}

type BatchAllInspector interface {
	BatchDiscoverAll(ctx context.Context, parentPath string) ([]DetectedProject, error)
}

// BatchDiscoverProjects scans child directories of a parked parent and discovers any PHP, Static or JS projects.
func (d Detector) BatchDiscoverProjects(ctx context.Context, parentPath string) ([]DetectedProject, error) {
	if d.Inspector == nil {
		return nil, fmt.Errorf("detector sem inspector configurado")
	}
	if batch, ok := d.Inspector.(BatchAllInspector); ok {
		return batch.BatchDiscoverAll(ctx, parentPath)
	}
	children, err := d.Inspector.ListDirectories(ctx, parentPath)
	if err != nil {
		return nil, err
	}
	results := make([]DetectedProject, 0, len(children))
	for _, child := range children {
		if detected, err := d.DetectProject(ctx, child); err == nil {
			results = append(results, detected)
		}
	}
	return results, nil
}

type RouteRecommendation struct {
	RecommendedMode domain.RouteMode
	Reason          string
}

// RecommendRouteMode advises the best route mode based on project architecture, framework, and runtime.
func RecommendRouteMode(detected DetectedProject) RouteRecommendation {
	switch detected.Kind {
	case ProjectKindDev:
		switch detected.JS.Framework {
		case "next", "nuxt", "remix", "sveltekit":
			return RouteRecommendation{
				RecommendedMode: domain.RouteModePort,
				Reason:          fmt.Sprintf("Framework %s utiliza roteamento na raiz e assets absolutos; o modo 'port' ou 'host' evita conflitos de subpath", detected.JS.Framework),
			}
		case "vite":
			return RouteRecommendation{
				RecommendedMode: domain.RouteModePort,
				Reason:          "Vite com HMR/WebSocket opera de forma mais transparente em modo 'port' ou 'host'",
			}
		default:
			return RouteRecommendation{
				RecommendedMode: domain.RouteModePort,
				Reason:          "Servidores de desenvolvimento JavaScript operam de forma ideal com porta dedicada (raiz /)",
			}
		}
	case ProjectKindStatic:
		if detected.JS.IsSPA {
			return RouteRecommendation{
				RecommendedMode: domain.RouteModePath,
				Reason:          "Projetos estáticos SPA funcionam bem em 'path' com fallback ou em 'host'",
			}
		}
		return RouteRecommendation{
			RecommendedMode: domain.RouteModePath,
			Reason:          "Projetos estáticos são compatíveis com modo 'path'",
		}
	case ProjectKindPHP:
		return RouteRecommendation{
			RecommendedMode: domain.RouteModePath,
			Reason:          "Aplicações PHP são suportadas nativamente no modo 'path' com regravação de base URL",
		}
	default:
		return RouteRecommendation{
			RecommendedMode: domain.RouteModePath,
			Reason:          "Modo padrão 'path' para compatibilidade LAN sem DNS",
		}
	}
}
