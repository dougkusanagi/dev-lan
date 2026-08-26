package platform

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type PHPInstallation struct {
	Version        string
	PHPBinary      string
	FPMBinary      string
	ComposerBinary string
	Extensions     []string
}

// PHPManager is deliberately small so the application can be tested without
// a live WSL distribution. Optional interfaces below add pools, Composer and
// logs without forcing every test double to implement every operation.
type PHPManager interface {
	List(ctx context.Context) ([]PHPInstallation, error)
	Install(ctx context.Context, version string, extensions []string) error
	Remove(ctx context.Context, version string) error
}

type PHPPoolSpec struct {
	Version    string
	Name       string
	FPMBinary  string
	ConfigPath string
}

type PHPPoolManager interface {
	EnsurePools(ctx context.Context, pools []PHPPoolSpec) error
	StopVersion(ctx context.Context, version string) error
}

type PHPComposerManager interface {
	RunComposer(ctx context.Context, version, environment, composerBinary string, args ...string) (string, error)
}

type PHPInfo struct {
	Version        string
	PHPBinary      string
	FPMBinary      string
	ComposerBinary string
	Extensions     []string
}

type PHPInfoManager interface {
	Info(ctx context.Context, version string) (PHPInfo, error)
	Logs(ctx context.Context, version string) (string, error)
}

type WSLPHPManager struct {
	WSL WSLRunner
}

func NewWSLPHPManager(wsl WSLRunner) WSLPHPManager { return WSLPHPManager{WSL: wsl} }

var phpVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
var phpExtensionPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.+-]{0,63}$`)

func normalizePHPVersion(value string) (string, error) {
	version := strings.TrimSpace(value)
	if !phpVersionPattern.MatchString(version) {
		return "", fmt.Errorf("versão PHP inválida %q", value)
	}
	return version, nil
}

func phpCommand(version string) string { return "php" + version }
func fpmCommand(version string) string { return "php-fpm" + version }

func (m WSLPHPManager) List(ctx context.Context) ([]PHPInstallation, error) {
	if m.WSL.Binary == "" {
		return nil, fmt.Errorf("%w: WSL não configurado", ErrUnavailable)
	}
	versions := []string{"8.5", "8.4", "8.3", "8.2", "8.1", "8.0", "7.4"}
	commands := make([]string, 0, len(versions)*2+1)
	for _, version := range versions {
		commands = append(commands, phpCommand(version), fpmCommand(version))
	}
	commands = append(commands, "php")
	found, err := m.WSL.HasCommands(ctx, commands...)
	if err != nil {
		return nil, err
	}
	result := make([]PHPInstallation, 0, len(versions))
	for _, version := range versions {
		php := found[phpCommand(version)]
		fpm := found[fpmCommand(version)]
		if !php && !fpm {
			continue
		}
		result = append(result, PHPInstallation{
			Version:        version,
			PHPBinary:      phpCommand(version),
			FPMBinary:      fpmCommand(version),
			ComposerBinary: "composer",
		})
	}
	// A distribution may expose only the unversioned binaries. Discover its
	// branch with a fixed PHP expression, never with project-provided code.
	if len(result) == 0 {
		if found["php"] {
			output, runErr := m.WSL.Run(ctx, "php", "-r", "echo PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;")
			if runErr == nil {
				if version, normalizeErr := normalizePHPVersion(strings.TrimSpace(output)); normalizeErr == nil {
					result = append(result, PHPInstallation{Version: version, PHPBinary: "php", FPMBinary: "php-fpm", ComposerBinary: "composer"})
				}
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result, nil
}

func validateExtensions(extensions []string) ([]string, error) {
	result := make([]string, 0, len(extensions))
	seen := map[string]struct{}{}
	for _, extension := range extensions {
		extension = strings.ToLower(strings.TrimSpace(extension))
		if extension == "" {
			continue
		}
		if !phpExtensionPattern.MatchString(extension) {
			return nil, fmt.Errorf("extensão PHP inválida: %q", extension)
		}
		if _, exists := seen[extension]; !exists {
			seen[extension] = struct{}{}
			result = append(result, extension)
		}
	}
	sort.Strings(result)
	return result, nil
}

func packageExtensionName(extension string) string {
	switch extension {
	case "pdo_mysql":
		return "mysql"
	case "pdo_pgsql":
		return "pgsql"
	default:
		return extension
	}
}

func (m WSLPHPManager) runAsRoot(ctx context.Context, args ...string) (string, error) {
	if m.WSL.Binary == "" {
		return "", fmt.Errorf("%w: WSL não configurado", ErrUnavailable)
	}
	return m.WSL.RunAsRoot(ctx, args...)
}

func (m WSLPHPManager) EnsureRepository(ctx context.Context, version string) error {
	// Check if package is already found in apt cache
	if out, err := m.runAsRoot(ctx, "apt-cache", "show", "php"+version+"-fpm"); err == nil && strings.Contains(out, "Package: php"+version+"-fpm") {
		return nil
	}

	// Ensure prerequisites
	if found, _ := m.WSL.HasCommand(ctx, "add-apt-repository"); !found {
		_, _ = m.runAsRoot(ctx, "apt-get", "update")
		_, _ = m.runAsRoot(ctx, "apt-get", "install", "-y", "software-properties-common", "ca-certificates", "curl", "gnupg")
	}

	// Try adding ppa:ondrej/php
	out, err := m.runAsRoot(ctx, "add-apt-repository", "-y", "ppa:ondrej/php")
	if err != nil || strings.Contains(out, "404") || strings.Contains(out, "does not have a Release file") {
		// If Launchpad PPA doesn't have a release file for this distribution codename, configure canonical packages.sury.org repository
		suryScript := "if [ ! -f /etc/apt/trusted.gpg.d/php.gpg ]; then curl -sSLo /etc/apt/trusted.gpg.d/php.gpg https://packages.sury.org/php/apt.gpg 2>/dev/null || true; fi; " +
			"if [ -f /etc/apt/trusted.gpg.d/php.gpg ] && [ ! -f /etc/apt/sources.list.d/php-sury.list ]; then echo 'deb [signed-by=/etc/apt/trusted.gpg.d/php.gpg] https://packages.sury.org/php/ trixie main' > /etc/apt/sources.list.d/php-sury.list; fi"
		_, _ = m.runAsRoot(ctx, "bash", "-c", suryScript)
	}
	_, _ = m.runAsRoot(ctx, "apt-get", "update")
	return nil
}

func (m WSLPHPManager) Install(ctx context.Context, version string, extensions []string) error {
	version, err := normalizePHPVersion(version)
	if err != nil {
		return err
	}
	extensions, err = validateExtensions(extensions)
	if err != nil {
		return err
	}

	_ = m.EnsureRepository(ctx, version)

	packages := []string{"php" + version + "-cli", "php" + version + "-fpm", "composer"}
	for _, extension := range extensions {
		packages = append(packages, "php"+version+"-"+packageExtensionName(extension))
	}
	args := append([]string{"apt-get", "install", "-y"}, packages...)
	if out, err := m.runAsRoot(ctx, args...); err != nil {
		if strings.Contains(out, "Unable to locate package") || strings.Contains(out, "Couldn't find any package") {
			return fmt.Errorf("a versão PHP %s não está disponível nos repositórios da distribuição Linux instalada no WSL. Verifique se a versão é suportada pelo Ubuntu/Debian", version)
		}
		return fmt.Errorf("instalar PHP %s: %w (%s)", version, err, strings.TrimSpace(out))
	}
	return nil
}

func (m WSLPHPManager) Remove(ctx context.Context, version string) error {
	version, err := normalizePHPVersion(version)
	if err != nil {
		return err
	}
	packages := []string{
		"php" + version + "-cli",
		"php" + version + "-fpm",
		"php" + version + "-mbstring",
		"php" + version + "-xml",
		"php" + version + "-curl",
		"php" + version + "-zip",
		"php" + version + "-mysql",
		"php" + version + "-pgsql",
		"php" + version + "-bcmath",
		"php" + version + "-intl",
		"php" + version + "-gd",
	}
	args := append([]string{"apt-get", "remove", "-y"}, packages...)
	if _, err := m.runAsRoot(ctx, args...); err != nil {
		return fmt.Errorf("remover PHP %s: %w", version, err)
	}
	return nil
}

func (m WSLPHPManager) EnsurePools(ctx context.Context, pools []PHPPoolSpec) error {
	for _, pool := range pools {
		version, err := normalizePHPVersion(pool.Version)
		if err != nil {
			return err
		}
		if strings.TrimSpace(pool.Name) == "" || strings.TrimSpace(pool.ConfigPath) == "" {
			return fmt.Errorf("pool PHP incompleto para %s", version)
		}
		configPath, err := ToWSLPath(pool.ConfigPath)
		if err != nil {
			return err
		}
		binary := pool.FPMBinary
		if binary == "" {
			binary = fpmCommand(version)
		}
		if _, err := m.runAsRoot(ctx, "/bin/mkdir", "-p", "/run/devlan/php/"+version, "/var/log/devlan"); err != nil {
			return fmt.Errorf("preparar diretórios do pool PHP %s: %w", version, err)
		}
		pidPath := "/run/devlan/php/" + version + "/php-fpm.pid"
		if exists, existsErr := m.WSL.Exists(ctx, pidPath); existsErr == nil && exists {
			if _, reloadErr := m.runAsRoot(ctx, "/usr/bin/pkill", "-USR2", "-F", pidPath); reloadErr == nil {
				continue
			}
		}
		// The generated master config has daemonize=yes. Running it through the
		// WSL root boundary starts an independent master for each PHP branch and
		// returns immediately without a shell or an interpolated command string.
		if _, err := m.runAsRoot(ctx, binary, "-y", configPath); err != nil {
			return fmt.Errorf("iniciar pool PHP %s/%s: %w", version, pool.Name, err)
		}
	}
	return nil
}

func (m WSLPHPManager) StopVersion(ctx context.Context, version string) error {
	version, err := normalizePHPVersion(version)
	if err != nil {
		return err
	}
	pidPath := "/run/devlan/php/" + version + "/php-fpm.pid"
	_, err = m.runAsRoot(ctx, "/usr/bin/pkill", "-TERM", "-F", pidPath)
	return err
}

func (m WSLPHPManager) RunComposer(ctx context.Context, version, environment, composerBinary string, args ...string) (string, error) {
	version, err := normalizePHPVersion(version)
	if err != nil {
		return "", err
	}
	if environment == "" || environment == "auto" {
		environment = "per-version"
	}
	if environment != "system" && environment != "per-version" {
		return "", fmt.Errorf("ambiente do Composer inválido: %s", environment)
	}
	if composerBinary == "" {
		composerBinary = "composer"
	}
	if strings.ContainsAny(composerBinary, "\r\n\t ") {
		return "", fmt.Errorf("binário do Composer inválido")
	}
	command := []string{composerBinary}
	if environment == "per-version" {
		composerScript := composerBinary
		if !strings.Contains(composerScript, "/") {
			resolved, resolveErr := m.WSL.Run(ctx, "/usr/bin/which", composerScript)
			if resolveErr != nil {
				return "", fmt.Errorf("localizar Composer: %w", resolveErr)
			}
			composerScript = strings.TrimSpace(resolved)
			if composerScript == "" || !strings.HasPrefix(composerScript, "/") {
				return "", fmt.Errorf("caminho do Composer inválido")
			}
		}
		command = []string{phpCommand(version), composerScript}
	}
	command = append(command, args...)
	return m.WSL.Run(ctx, command...)
}

func (m WSLPHPManager) Info(ctx context.Context, version string) (PHPInfo, error) {
	version, err := normalizePHPVersion(version)
	if err != nil {
		return PHPInfo{}, err
	}
	installation := PHPInfo{Version: version, PHPBinary: phpCommand(version), FPMBinary: fpmCommand(version), ComposerBinary: "composer"}
	if found, err := m.WSL.HasCommand(ctx, installation.PHPBinary); err != nil {
		return PHPInfo{}, err
	} else if !found {
		return PHPInfo{}, fmt.Errorf("PHP %s não instalado", version)
	}
	output, err := m.WSL.Run(ctx, installation.PHPBinary, "-m")
	if err == nil {
		lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "[") || line == "]" {
				continue
			}
			if phpExtensionPattern.MatchString(line) {
				installation.Extensions = append(installation.Extensions, strings.ToLower(line))
			}
		}
		sort.Strings(installation.Extensions)
	}
	return installation, nil
}

func (m WSLPHPManager) Logs(ctx context.Context, version string) (string, error) {
	version, err := normalizePHPVersion(version)
	if err != nil {
		return "", err
	}
	return m.WSL.Run(ctx, "/bin/cat", "/var/log/devlan/php-"+version+"-fpm.log")
}
