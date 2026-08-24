package detect

import (
	"context"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

type Inspector interface {
	Exists(ctx context.Context, projectPath, relativePath string) (bool, error)
	Directory(ctx context.Context, projectPath string) (bool, error)
	ListDirectories(ctx context.Context, projectPath string) ([]string, error)
	ReadFile(ctx context.Context, projectPath, relativePath string) ([]byte, error)
}

type LaravelResult struct {
	ProjectPath string
	Artisan     bool
	PublicIndex bool
}

type PHPResult struct {
	ProjectPath  string
	Preset       domain.PHPPreset
	DocumentRoot string
	Artisan      bool
	Console      bool
	PublicIndex  bool
	RootIndex    bool
}

type Detector struct {
	Inspector Inspector
}

type LaravelInspector interface {
	DetectLaravel(ctx context.Context, projectPath string) (LaravelResult, error)
}

type BatchInspector interface {
	BatchDiscoverPHP(ctx context.Context, parentPath string) ([]PHPResult, error)
}

func (d Detector) BatchDetectPHP(ctx context.Context, parentPath string) ([]PHPResult, error) {
	if d.Inspector == nil {
		return nil, fmt.Errorf("detector sem inspector configurado")
	}
	if batch, ok := d.Inspector.(BatchInspector); ok {
		return batch.BatchDiscoverPHP(ctx, parentPath)
	}
	children, err := d.Inspector.ListDirectories(ctx, parentPath)
	if err != nil {
		return nil, err
	}
	results := make([]PHPResult, 0, len(children))
	for _, child := range children {
		if res, err := d.DetectPHP(ctx, child); err == nil {
			results = append(results, res)
		}
	}
	return results, nil
}

func (d Detector) Detect(ctx context.Context, projectPath string) (LaravelResult, error) {
	if d.Inspector == nil {
		return LaravelResult{}, fmt.Errorf("detector sem inspector configurado")
	}
	if inspector, ok := d.Inspector.(LaravelInspector); ok {
		return inspector.DetectLaravel(ctx, projectPath)
	}
	artisan, err := d.Inspector.Exists(ctx, projectPath, "artisan")
	if err != nil {
		return LaravelResult{}, err
	}
	index, err := d.Inspector.Exists(ctx, projectPath, "public/index.php")
	if err != nil {
		return LaravelResult{}, err
	}
	result := LaravelResult{ProjectPath: projectPath, Artisan: artisan, PublicIndex: index}
	if !artisan || !index {
		missing := make([]string, 0, 2)
		if !artisan {
			missing = append(missing, "artisan")
		}
		if !index {
			missing = append(missing, "public/index.php")
		}
		return result, fmt.Errorf("projeto não parece Laravel; ausente: %s", strings.Join(missing, ", "))
	}
	return result, nil
}

// DetectPHP recognizes the three supported PHP presets using marker files
// only. No composer or application script is executed during discovery.
func (d Detector) DetectPHP(ctx context.Context, projectPath string) (PHPResult, error) {
	if d.Inspector == nil {
		return PHPResult{}, fmt.Errorf("detector sem inspector configurado")
	}
	markers := PHPResult{ProjectPath: projectPath}
	var err error
	if markers.Artisan, err = d.Inspector.Exists(ctx, projectPath, "artisan"); err != nil {
		return PHPResult{}, err
	}
	if markers.Console, err = d.Inspector.Exists(ctx, projectPath, "bin/console"); err != nil {
		return PHPResult{}, err
	}
	if markers.PublicIndex, err = d.Inspector.Exists(ctx, projectPath, "public/index.php"); err != nil {
		return PHPResult{}, err
	}
	if markers.RootIndex, err = d.Inspector.Exists(ctx, projectPath, "index.php"); err != nil {
		return PHPResult{}, err
	}
	switch {
	case markers.Artisan && markers.PublicIndex:
		markers.Preset = domain.PHPPresetLaravel
		markers.DocumentRoot = pathpkg.Join(projectPath, "public")
	case markers.Console && markers.PublicIndex:
		markers.Preset = domain.PHPPresetSymfony
		markers.DocumentRoot = pathpkg.Join(projectPath, "public")
	case markers.PublicIndex:
		markers.Preset = domain.PHPPresetGeneric
		markers.DocumentRoot = pathpkg.Join(projectPath, "public")
	case markers.RootIndex:
		markers.Preset = domain.PHPPresetGeneric
		markers.DocumentRoot = projectPath
	default:
		missing := []string{"public/index.php ou index.php"}
		return markers, fmt.Errorf("projeto PHP não reconhecido; ausente: %s", strings.Join(missing, ", "))
	}
	return markers, nil
}

type LocalInspector struct{}

func (LocalInspector) Exists(_ context.Context, projectPath, relativePath string) (bool, error) {
	file := filepath.FromSlash(pathpkg.Join(projectPath, relativePath))
	info, err := os.Stat(file)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
}

func (LocalInspector) Directory(_ context.Context, projectPath string) (bool, error) {
	info, err := os.Stat(filepath.FromSlash(projectPath))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func (LocalInspector) ReadFile(_ context.Context, projectPath, relativePath string) ([]byte, error) {
	file := filepath.FromSlash(pathpkg.Join(projectPath, relativePath))
	return os.ReadFile(file)
}

func (l LocalInspector) ListDirectories(_ context.Context, projectPath string) ([]string, error) {
	entries, err := os.ReadDir(filepath.FromSlash(projectPath))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			paths = append(paths, pathpkg.Join(projectPath, entry.Name()))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (l LocalInspector) BatchDiscoverPHP(ctx context.Context, parentPath string) ([]PHPResult, error) {
	children, err := l.ListDirectories(ctx, parentPath)
	if err != nil {
		return nil, err
	}
	results := make([]PHPResult, 0, len(children))
	for _, child := range children {
		if res, err := (Detector{Inspector: l}).DetectPHP(ctx, child); err == nil {
			results = append(results, res)
		}
	}
	return results, nil
}

type SmartInspector struct {
	Local LocalInspector
	WSL   platform.WSLRunner
}

func (s SmartInspector) usesWSL(projectPath string) bool {
	return runtime.GOOS == "windows" && strings.HasPrefix(projectPath, "/")
}

func (s SmartInspector) Exists(ctx context.Context, projectPath, relativePath string) (bool, error) {
	if s.usesWSL(projectPath) {
		return s.WSL.Exists(ctx, pathpkg.Join(projectPath, relativePath))
	}
	return s.Local.Exists(ctx, projectPath, relativePath)
}

func (s SmartInspector) ReadFile(ctx context.Context, projectPath, relativePath string) ([]byte, error) {
	if s.usesWSL(projectPath) {
		return s.WSL.ReadFile(ctx, pathpkg.Join(projectPath, relativePath))
	}
	return s.Local.ReadFile(ctx, projectPath, relativePath)
}

func (s SmartInspector) Directory(ctx context.Context, projectPath string) (bool, error) {
	if s.usesWSL(projectPath) {
		return s.WSL.Exists(ctx, projectPath)
	}
	return s.Local.Directory(ctx, projectPath)
}

func (s SmartInspector) ListDirectories(ctx context.Context, projectPath string) ([]string, error) {
	if s.usesWSL(projectPath) {
		return s.WSL.ListDirectories(ctx, projectPath)
	}
	return s.Local.ListDirectories(ctx, projectPath)
}

func (s SmartInspector) BatchDiscoverPHP(ctx context.Context, projectPath string) ([]PHPResult, error) {
	if s.usesWSL(projectPath) {
		raw, err := s.WSL.DiscoverAllProjects(ctx, projectPath)
		if err != nil {
			return nil, err
		}
		results := make([]PHPResult, 0, len(raw))
		for _, d := range raw {
			if !d.Artisan && !d.PublicIndex && !d.RootIndex && !d.Console {
				continue
			}
			res := PHPResult{
				ProjectPath:  d.Path,
				Artisan:      d.Artisan,
				PublicIndex:  d.PublicIndex,
				RootIndex:    d.RootIndex,
				Console:      d.Console,
			}
			switch {
			case d.Artisan && d.PublicIndex:
				res.Preset = domain.PHPPresetLaravel
				res.DocumentRoot = pathpkg.Join(d.Path, "public")
				results = append(results, res)
			case d.Console && d.PublicIndex:
				res.Preset = domain.PHPPresetSymfony
				res.DocumentRoot = pathpkg.Join(d.Path, "public")
				results = append(results, res)
			case d.PublicIndex:
				res.Preset = domain.PHPPresetGeneric
				res.DocumentRoot = pathpkg.Join(d.Path, "public")
				results = append(results, res)
			case d.RootIndex:
				res.Preset = domain.PHPPresetGeneric
				res.DocumentRoot = d.Path
				results = append(results, res)
			}
		}
		return results, nil
	}
	return s.Local.BatchDiscoverPHP(ctx, projectPath)
}

func (s SmartInspector) BatchDiscoverAll(ctx context.Context, projectPath string) ([]DetectedProject, error) {
	if s.usesWSL(projectPath) {
		raw, err := s.WSL.DiscoverAllProjects(ctx, projectPath)
		if err != nil {
			return nil, err
		}
		results := make([]DetectedProject, 0, len(raw))
		for _, r := range raw {
			if (r.Artisan && r.PublicIndex) || (r.Console && r.PublicIndex) || r.PublicIndex || r.RootIndex {
				phpRes := PHPResult{
					ProjectPath:  r.Path,
					Artisan:      r.Artisan,
					PublicIndex:  r.PublicIndex,
					RootIndex:    r.RootIndex,
					Console:      r.Console,
				}
				switch {
				case r.Artisan && r.PublicIndex:
					phpRes.Preset = domain.PHPPresetLaravel
					phpRes.DocumentRoot = pathpkg.Join(r.Path, "public")
				case r.Console && r.PublicIndex:
					phpRes.Preset = domain.PHPPresetSymfony
					phpRes.DocumentRoot = pathpkg.Join(r.Path, "public")
				case r.PublicIndex:
					phpRes.Preset = domain.PHPPresetGeneric
					phpRes.DocumentRoot = pathpkg.Join(r.Path, "public")
				case r.RootIndex:
					phpRes.Preset = domain.PHPPresetGeneric
					phpRes.DocumentRoot = r.Path
				}
				results = append(results, DetectedProject{
					ProjectPath:   r.Path,
					Kind:          ProjectKindPHP,
					SuggestedMode: domain.ModePHP,
					PHP:           phpRes,
				})
				continue
			}

			if r.HasPackageJSON || r.DistHTML || r.DistDir || r.RootHTML {
				pm := "npm"
				switch {
				case r.PnpmLock:
					pm = "pnpm"
				case r.YarnLock:
					pm = "yarn"
				case r.BunLock:
					pm = "bun"
				case r.NpmLock:
					pm = "npm"
				}

				fw := "generic"
				switch {
				case r.Next:
					fw = "next"
				case r.Nuxt:
					fw = "nuxt"
				case r.Astro:
					fw = "astro"
				case r.Svelte:
					fw = "sveltekit"
				case r.Vite:
					fw = "vite"
				}

				staticDir := ""
				if r.DistHTML || r.DistDir {
					staticDir = "dist"
				}

				jsRes := JSResult{
					ProjectPath:    r.Path,
					HasPackageJSON: r.HasPackageJSON,
					PackageManager: pm,
					Framework:      fw,
					DevScript:      pmDevCommand(pm, "dev"),
					BuildScript:    pmBuildCommand(pm, "build"),
					StaticDir:      staticDir,
					HasStaticBuild: r.DistHTML || r.DistDir || r.RootHTML,
					IsSPA:          r.DistHTML || r.RootHTML,
					HasDevServer:   r.HasPackageJSON,
				}

				kind := ProjectKindStatic
				suggested := domain.ModeStatic
				if r.HasPackageJSON {
					kind = ProjectKindDev
					suggested = domain.ModeDev
				}

				results = append(results, DetectedProject{
					ProjectPath:   r.Path,
					Kind:          kind,
					SuggestedMode: suggested,
					JS:            jsRes,
				})
			}
		}
		return results, nil
	}
	return (Detector{Inspector: s.Local}).BatchDiscoverProjects(ctx, projectPath)
}

func (s SmartInspector) DetectLaravel(ctx context.Context, projectPath string) (LaravelResult, error) {
	if s.usesWSL(projectPath) {
		artisan, index, err := s.WSL.LaravelMarkers(ctx, projectPath)
		if err != nil {
			return LaravelResult{}, err
		}
		result := LaravelResult{ProjectPath: projectPath, Artisan: artisan, PublicIndex: index}
		if artisan && index {
			return result, nil
		}
		missing := make([]string, 0, 2)
		if !artisan {
			missing = append(missing, "artisan")
		}
		if !index {
			missing = append(missing, "public/index.php")
		}
		return result, fmt.Errorf("projeto não parece Laravel; ausente: %s", strings.Join(missing, ", "))
	}
	return Detector{Inspector: s.Local}.Detect(ctx, projectPath)
}

func (s SmartInspector) DetectPHP(ctx context.Context, projectPath string) (PHPResult, error) {
	if s.usesWSL(projectPath) {
		markers := PHPResult{ProjectPath: projectPath}
		var err error
		if markers.Artisan, markers.PublicIndex, err = s.WSL.LaravelMarkers(ctx, projectPath); err != nil {
			return PHPResult{}, err
		}
		// The WSL marker call above is intentionally kept batched for the common
		// Laravel case. The remaining markers are individual safe test calls.
		if markers.Artisan && markers.PublicIndex {
			markers.Preset = domain.PHPPresetLaravel
			markers.DocumentRoot = pathpkg.Join(projectPath, "public")
			return markers, nil
		}
		var exists bool
		if exists, err = s.WSL.Exists(ctx, pathpkg.Join(projectPath, "bin/console")); err != nil {
			return PHPResult{}, err
		}
		markers.Console = exists
		if exists, err = s.WSL.Exists(ctx, pathpkg.Join(projectPath, "public/index.php")); err != nil {
			return PHPResult{}, err
		}
		markers.PublicIndex = exists
		if exists, err = s.WSL.Exists(ctx, pathpkg.Join(projectPath, "index.php")); err != nil {
			return PHPResult{}, err
		}
		markers.RootIndex = exists
		switch {
		case markers.Console && markers.PublicIndex:
			markers.Preset = domain.PHPPresetSymfony
			markers.DocumentRoot = pathpkg.Join(projectPath, "public")
		case markers.PublicIndex:
			markers.Preset = domain.PHPPresetGeneric
			markers.DocumentRoot = pathpkg.Join(projectPath, "public")
		case markers.RootIndex:
			markers.Preset = domain.PHPPresetGeneric
			markers.DocumentRoot = projectPath
		default:
			return markers, fmt.Errorf("projeto PHP não reconhecido; ausente: public/index.php ou index.php")
		}
		return markers, nil
	}
	return Detector{Inspector: s.Local}.DetectPHP(ctx, projectPath)
}

// StaticInspector makes service tests independent of the host filesystem.
type StaticInspector struct {
	Directories  map[string]bool
	Files        map[string]bool
	FileContents map[string]string
	Children     map[string][]string
}

func (s StaticInspector) Exists(_ context.Context, projectPath, relativePath string) (bool, error) {
	return s.Files[pathpkg.Join(projectPath, relativePath)], nil
}

func (s StaticInspector) ReadFile(_ context.Context, projectPath, relativePath string) ([]byte, error) {
	p := pathpkg.Join(projectPath, relativePath)
	if content, ok := s.FileContents[p]; ok {
		return []byte(content), nil
	}
	if s.Files[p] {
		return []byte("{}"), nil
	}
	return nil, os.ErrNotExist
}

func (s StaticInspector) Directory(_ context.Context, projectPath string) (bool, error) {
	return s.Directories[projectPath], nil
}

func (s StaticInspector) ListDirectories(_ context.Context, projectPath string) ([]string, error) {
	children := append([]string(nil), s.Children[projectPath]...)
	sort.Strings(children)
	return children, nil
}
