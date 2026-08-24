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

func (LocalInspector) ListDirectories(_ context.Context, projectPath string) ([]string, error) {
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
	Directories map[string]bool
	Files       map[string]bool
	Children    map[string][]string
}

func (s StaticInspector) Exists(_ context.Context, projectPath, relativePath string) (bool, error) {
	return s.Files[pathpkg.Join(projectPath, relativePath)], nil
}

func (s StaticInspector) Directory(_ context.Context, projectPath string) (bool, error) {
	return s.Directories[projectPath], nil
}

func (s StaticInspector) ListDirectories(_ context.Context, projectPath string) ([]string, error) {
	children := append([]string(nil), s.Children[projectPath]...)
	sort.Strings(children)
	return children, nil
}
