package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

const version = "0.1.0-mvp"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	dataDir, commandArgs, err := parseGlobalArgs(args)
	if err != nil {
		return err
	}
	if len(commandArgs) == 0 {
		printUsage()
		return nil
	}
	if commandArgs[0] == "help" || commandArgs[0] == "--help" || commandArgs[0] == "-h" {
		printUsage()
		return nil
	}
	if commandArgs[0] == "version" || commandArgs[0] == "--version" {
		fmt.Println(version)
		return nil
	}

	service := app.New(dataDir)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := commandArgs[0]
	args = commandArgs[1:]
	if containsHelp(args) {
		printCommandUsage(command)
		return nil
	}

	switch command {
	case "install":
		configureFirewall := true
		windowsPort := 0
		for index := 0; index < len(args); index++ {
			argument := args[index]
			if argument == "--no-firewall" {
				configureFirewall = false
				continue
			}
			if argument == "--windows-port" && index+1 < len(args) {
				index++
				parsed, err := strconv.Atoi(args[index])
				if err != nil || parsed < 1 || parsed > 65535 {
					return fmt.Errorf("porta Windows inválida: %q", args[index])
				}
				windowsPort = parsed
				continue
			}
			return fmt.Errorf("uso: devlan install [--no-firewall] [--windows-port PORT]")
		}
		result, err := service.InstallWithPort(ctx, configureFirewall, windowsPort)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("DevLAN inicializado em %s\n", dataDir)
		fmt.Println("Componentes externos são verificados por `devlan doctor`.")
		return nil

	case "uninstall":
		if len(args) != 0 {
			return fmt.Errorf("uso: devlan uninstall")
		}
		result, err := service.Uninstall(ctx)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Println("Arquivos gerenciados removidos; diretórios dos projetos foram preservados.")
		return nil

	case "link":
		if len(args) != 2 {
			return fmt.Errorf("uso: devlan link NAME PATH")
		}
		project, result, err := service.Link(ctx, args[0], args[1])
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Projeto %s registrado: %s\n", project.Name, project.Path)
		return nil

	case "unlink":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan unlink NAME")
		}
		project, result, err := service.Unlink(ctx, args[0])
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Projeto %s removido do registro.\n", project.Name)
		return nil

	case "links":
		asJSON := false
		filter := ""
		for _, arg := range args {
			if arg == "--json" {
				asJSON = true
			} else if !strings.HasPrefix(arg, "-") && filter == "" {
				filter = arg
			} else {
				return fmt.Errorf("uso: devlan links [FILTRO] [--json]")
			}
		}
		return printProjects(service, filter, asJSON)

	case "park":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan park PATH")
		}
		park, result, err := service.Park(ctx, args[0])
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Diretório estacionado: %s\n", park.Path)
		return nil

	case "unpark":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan unpark PATH")
		}
		park, result, err := service.Unpark(ctx, args[0])
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Diretório removido dos estacionados: %s\n", park.Path)
		return nil

	case "parked":
		if len(args) != 0 {
			return fmt.Errorf("uso: devlan parked")
		}
		cfg, err := service.Store.Load()
		if err != nil {
			return err
		}
		for _, park := range cfg.Parks {
			if park.Mode == nil {
				fmt.Printf("%s (herda %s)\n", park.Path, cfg.DefaultMode)
			} else {
				fmt.Printf("%s (%s)\n", park.Path, *park.Mode)
			}
		}
		return nil

	case "mode":
		return runMode(ctx, service, args)

	case "php":
		return runPHP(ctx, service, args)

	case "composer":
		return runComposer(ctx, service, args)

	case "status":
		if len(args) != 0 {
			return fmt.Errorf("uso: devlan status")
		}
		return printStatus(ctx, service, dataDir)

	case "reload":
		if len(args) != 0 {
			return fmt.Errorf("uso: devlan reload")
		}
		result, err := service.Reload(ctx)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Println("Configurações geradas, validadas quando possível e recarregadas quando o Caddy está disponível.")
		return nil

	case "trust":
		if len(args) != 0 {
			return fmt.Errorf("uso: devlan trust")
		}
		if err := service.Trust(ctx); err != nil {
			return fmt.Errorf("não foi possível confiar na CA local automaticamente (%w); execute `devlan trust` em PowerShell como Administrador", err)
		}
		fmt.Println("Certificado raiz local do Caddy instalado e confiado no sistema.")
		return nil

	case "secure", "unsecure":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan %s NAME|PATH", command)
		}
		enabled := command == "secure"
		result, projectName, err := service.SetProjectTLS(ctx, args[0], enabled)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		if enabled {
			fmt.Printf("SSL ativado para %s com a CA interna do Caddy.\n", projectName)
			fmt.Println("Outros dispositivos da LAN precisam confiar na CA local do DevLAN.")
		} else {
			fmt.Printf("SSL desativado para %s.\n", projectName)
			fmt.Println("[dica] Se o navegador apresentar erros de CSRF ou cookies rejeitados, limpe os cookies ou acesse a versão HTTPS uma vez para redefinir o estado.")
		}
		return nil

	case "doctor":
		if len(args) > 1 {
			return fmt.Errorf("uso: devlan doctor [NAME]")
		}
		name := ""
		if len(args) == 1 {
			name = args[0]
		}
		checks, err := service.Doctor(ctx, name)
		if err != nil {
			return err
		}
		for _, check := range checks {
			fmt.Printf("[%s] %-20s %s\n", check.Status, check.Name, check.Detail)
		}
		return nil

	case "logs":
		if len(args) > 1 {
			return fmt.Errorf("uso: devlan logs [COMPONENT]")
		}
		component := ""
		if len(args) == 1 {
			component = args[0]
		}
		logs, err := service.Logs(component)
		if err != nil {
			return err
		}
		fmt.Print(logs)
		return nil

	case "open":
		if len(args) > 1 {
			return fmt.Errorf("uso: devlan open [NAME]")
		}
		if len(args) == 0 {
			return printProjects(service, "", false)
		}
		url, err := service.Open(ctx, args[0])
		fmt.Println(url)
		if err != nil {
			return fmt.Errorf("URL calculada, mas não foi possível abrir o navegador: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("comando desconhecido %q; use `devlan help`", command)
	}
}

func parseGlobalArgs(args []string) (string, []string, error) {
	dataDir := defaultDataDir()
	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--data-dir":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return "", nil, fmt.Errorf("--data-dir exige um caminho")
			}
			dataDir = args[i+1]
			i++
		case "--version", "-v", "--help", "-h":
			remaining = append(remaining, args[i])
		default:
			remaining = append(remaining, args[i])
		}
	}
	return filepath.Clean(dataDir), remaining, nil
}

func defaultDataDir() string {
	if configured := strings.TrimSpace(os.Getenv("DEVLAN_HOME")); configured != "" {
		return configured
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		return filepath.Join(localAppData, "DevLAN")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".devlan")
	}
	return ".devlan"
}

func runMode(ctx context.Context, service *app.App, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("uso: devlan mode default php | devlan mode NAME php|inherit")
	}
	if args[0] == "default" {
		mode, err := domain.ParseMode(args[1])
		if err != nil {
			return err
		}
		result, err := service.SetDefaultMode(ctx, mode)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Modo global definido como %s.\n", mode)
		return nil
	}
	if args[1] == "inherit" {
		result, err := service.SetProjectMode(ctx, args[0], nil)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Projeto %s agora herda o modo efetivo.\n", args[0])
		return nil
	}
	mode, err := domain.ParseMode(args[1])
	if err != nil {
		return err
	}
	result, err := service.SetProjectMode(ctx, args[0], &mode)
	printWarnings(result.Warnings)
	if err != nil {
		return err
	}
	fmt.Printf("Modo do projeto %s definido como %s.\n", args[0], mode)
	return nil
}

func runPHP(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		if len(args) > 1 {
			return fmt.Errorf("uso: devlan php list")
		}
		versions, err := service.PHPVersions(ctx)
		if err != nil {
			return err
		}
		cfg, err := service.Store.Load()
		if err != nil {
			return err
		}
		fmt.Printf("PADRÃO PHP: %s\n", cfg.PHPDefaultVersion)
		if len(versions) == 0 {
			fmt.Println("Nenhuma versão PHP registrada ou detectada.")
			return nil
		}
		fmt.Println("VERSÃO\tSTATUS\tEXTENSÕES")
		for _, version := range versions {
			status := "detectada"
			if version.Configured && version.Installed {
				status = "ativa"
			} else if version.Configured {
				status = "configurada"
			}
			fmt.Printf("%s\t%s\t%s\n", version.Version, status, strings.Join(version.Extensions, ","))
		}
		return nil
	}
	switch args[0] {
	case "install":
		if len(args) < 2 {
			return fmt.Errorf("uso: devlan php install VERSION [--extensions EXT1,EXT2]")
		}
		extensions, err := parseExtensionsFlag(args[2:])
		if err != nil {
			return err
		}
		result, err := service.PHPInstall(ctx, args[1], extensions)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("PHP %s instalado e registrado.\n", args[1])
		return nil

	case "remove", "uninstall":
		if len(args) != 2 {
			return fmt.Errorf("uso: devlan php remove VERSION")
		}
		result, err := service.PHPRemove(ctx, args[1])
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("PHP %s removido.\n", args[1])
		return nil

	case "use":
		if len(args) != 3 {
			return fmt.Errorf("uso: devlan php use default VERSION | devlan php use NAME VERSION|inherit")
		}
		var result app.ApplyResult
		var err error
		if args[1] == "default" {
			result, err = service.SetDefaultPHPVersion(ctx, args[2])
		} else {
			result, err = service.SetProjectPHPVersion(ctx, args[1], args[2])
		}
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Println("Preferência de versão PHP atualizada.")
		return nil

	case "extensions", "ext":
		if len(args) < 2 {
			return fmt.Errorf("uso: devlan php extensions VERSION [EXT1 EXT2 ...]")
		}
		if len(args) == 2 {
			cfg, err := service.Store.Load()
			if err != nil {
				return err
			}
			version, found := cfg.PHPVersion(args[1])
			if !found {
				return fmt.Errorf("versão PHP não registrada: %s", args[1])
			}
			fmt.Println(strings.Join(version.Extensions, "\n"))
			return nil
		}
		extensions, err := parseExtensionValues(args[2:])
		if err != nil {
			return err
		}
		result, err := service.SetPHPVersionExtensions(ctx, args[1], extensions)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Extensões de PHP %s atualizadas.\n", args[1])
		return nil

	case "pool":
		return runPHPPool(ctx, service, args[1:])

	case "preset":
		if len(args) != 3 {
			return fmt.Errorf("uso: devlan php preset NAME laravel|symfony|generic|inherit")
		}
		result, err := service.SetProjectPHPPreset(ctx, args[1], args[2])
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Preset PHP do projeto %s atualizado.\n", args[1])
		return nil

	case "info":
		if len(args) > 2 {
			return fmt.Errorf("uso: devlan php info [NAME]")
		}
		selector := ""
		if len(args) == 2 {
			selector = args[1]
		}
		page, err := service.PHPInfo(ctx, selector)
		if err != nil {
			return err
		}
		fmt.Print(page)
		return nil
	default:
		return fmt.Errorf("subcomando PHP desconhecido %q; use `devlan php --help`", args[0])
	}
}

func parseExtensionsFlag(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, nil
	}
	if len(args) != 2 || args[0] != "--extensions" {
		return nil, fmt.Errorf("uso: devlan php install VERSION [--extensions EXT1,EXT2]")
	}
	return parseExtensionValues([]string{args[1]})
}

func parseExtensionValues(values []string) ([]string, error) {
	result := make([]string, 0)
	for _, value := range values {
		for _, extension := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
			if strings.TrimSpace(extension) != "" {
				result = append(result, strings.TrimSpace(extension))
			}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("ao menos uma extensão é obrigatória")
	}
	return result, nil
}

func runPHPPool(ctx context.Context, service *app.App, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: devlan php pool default|VERSION|NAME [shared|isolated] [opções]")
	}
	if args[0] == "default" {
		pool, err := parsePoolOptions(args[1:], domain.DefaultPHPFPMPoolConfig())
		if err != nil {
			return err
		}
		result, err := service.SetPHPGlobalPool(ctx, pool)
		printWarnings(result.Warnings)
		return err
	}
	if len(args) >= 2 && (args[1] == "shared" || args[1] == "isolated") {
		result, err := service.SetProjectPHPIsolated(ctx, args[0], args[1] == "isolated")
		printWarnings(result.Warnings)
		return err
	}
	cfg, err := service.Store.Load()
	if err != nil {
		return err
	}
	version, found := cfg.PHPVersion(args[0])
	if !found {
		return fmt.Errorf("versão PHP não registrada: %s", args[0])
	}
	base := version.Pool
	if base.IsZero() {
		base = cfg.PHPFPMPool
	}
	pool, err := parsePoolOptions(args[1:], base)
	if err != nil {
		return err
	}
	result, err := service.SetPHPVersionPool(ctx, args[0], pool)
	printWarnings(result.Warnings)
	return err
}

func parsePoolOptions(args []string, base domain.PHPFPMPoolConfig) (domain.PHPFPMPoolConfig, error) {
	if err := base.Normalize(); err != nil {
		return domain.PHPFPMPoolConfig{}, err
	}
	for index := 0; index < len(args); index++ {
		if index+1 >= len(args) {
			return domain.PHPFPMPoolConfig{}, fmt.Errorf("opção %s exige um valor", args[index])
		}
		value := args[index+1]
		switch args[index] {
		case "--max-children":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return domain.PHPFPMPoolConfig{}, fmt.Errorf("--max-children inválido: %s", value)
			}
			base.MaxChildren = parsed
		case "--idle-timeout":
			base.IdleTimeout = value
		case "--max-requests":
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return domain.PHPFPMPoolConfig{}, fmt.Errorf("--max-requests inválido: %s", value)
			}
			base.MaxRequests = parsed
		default:
			return domain.PHPFPMPoolConfig{}, fmt.Errorf("opção de pool desconhecida: %s", args[index])
		}
		index++
	}
	if err := base.Normalize(); err != nil {
		return domain.PHPFPMPoolConfig{}, err
	}
	return base, nil
}

func runComposer(ctx context.Context, service *app.App, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: devlan composer VERSION|NAME [--environment auto|system|per-version] -- ARGUMENTOS | composer config default|NAME ENV")
	}
	if args[0] == "config" {
		if len(args) != 3 {
			return fmt.Errorf("uso: devlan composer config default|NAME auto|system|per-version")
		}
		result, err := service.SetComposerEnvironment(ctx, args[1], args[2])
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Println("Ambiente do Composer atualizado.")
		return nil
	}
	selector := args[0]
	environment := ""
	composerArgs := make([]string, 0, len(args)-1)
	for index := 1; index < len(args); index++ {
		if args[index] == "--environment" {
			if index+1 >= len(args) {
				return fmt.Errorf("--environment exige um valor")
			}
			environment = args[index+1]
			index++
			continue
		}
		if args[index] == "--" {
			composerArgs = append(composerArgs, args[index+1:]...)
			break
		}
		composerArgs = append(composerArgs, args[index])
	}
	output, err := service.RunComposer(ctx, selector, environment, composerArgs)
	if output != "" {
		fmt.Print(output)
		if !strings.HasSuffix(output, "\n") {
			fmt.Println()
		}
	}
	return err
}

func printProjects(service *app.App, filter string, asJSON bool) error {
	cfg, err := service.Store.Load()
	if err != nil {
		return err
	}
	effective, err := service.EffectiveConfig(context.Background(), cfg)
	if err != nil {
		return err
	}
	host := cfg.LANAddress
	if host == "auto" {
		host, err = platform.LANAddress()
		if err != nil {
			host = "localhost"
		}
	}
	if len(effective.Projects) == 0 {
		if asJSON {
			fmt.Println("[]")
			return nil
		}
		fmt.Println("Nenhum projeto registrado.")
		return nil
	}
	rows := make([]projectRow, 0, len(effective.Projects))
	filterLower := strings.ToLower(strings.TrimSpace(filter))
	for _, project := range effective.Projects {
		if filterLower != "" && !strings.Contains(strings.ToLower(project.Name), filterLower) && !strings.Contains(strings.ToLower(project.Path), filterLower) {
			continue
		}
		resolved, err := effective.Resolve(project.Name)
		if err != nil {
			return err
		}
		url := resolved.URL(host, cfg.WindowsPort, cfg.HTTPSPort, effective.SecureProject(project))
		phpVersion := effective.EffectivePHPVersion(project)
		preset := string(effective.PHPProjectPreset(project))
		rows = append(rows, projectRow{
			Name:   project.Name,
			Mode:   string(resolved.Mode),
			PHP:    phpVersion,
			Preset: preset,
			Source: string(resolved.Source),
			SSL:    sslState(cfg.SecureProject(project)),
			URL:    url,
			Path:   project.Path,
		})
	}
	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(rows)
	}
	if len(rows) == 0 {
		fmt.Printf("Nenhum projeto encontrado para o filtro %q.\n", filter)
		return nil
	}
	if err := writeProjectTable(os.Stdout, rows); err != nil {
		return err
	}
	fmt.Printf("\n💡 Dica: execute `devlan open %s` para abrir no navegador ou `devlan secure %s` para ativar HTTPS.\n", rows[0].Name, rows[0].Name)
	return nil
}

type projectRow struct {
	Name   string `json:"name"`
	Mode   string `json:"mode"`
	PHP    string `json:"php"`
	Preset string `json:"preset"`
	Source string `json:"source"`
	SSL    string `json:"ssl"`
	URL    string `json:"url"`
	Path   string `json:"path"`
}

func writeProjectTable(output io.Writer, rows []projectRow) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "PROJETO\tMODO\tPHP\tTIPO\tORIGEM\tSSL\tURL\tCAMINHO"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "-------\t----\t---\t----\t------\t---\t---\t-------"); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.Name, row.Mode, row.PHP, row.Preset, row.Source, row.SSL, row.URL, row.Path); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func sslState(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func printStatus(ctx context.Context, service *app.App, dataDir string) error {
	cfg, err := service.Store.Load()
	if err != nil {
		return err
	}
	if current, generated, diverged := service.CheckLANAddressDivergence(); diverged {
		fmt.Fprintf(os.Stderr, "[aviso] O IP da rede local mudou de %s para %s. Execute `devlan reload` para atualizar o Caddy.\n", generated, current)
	}
	fmt.Printf("DevLAN %s (%s)\n", version, app.RuntimeDescription())
	fmt.Printf("Dados: %s\n", dataDir)
	fmt.Printf("Padrão: %s | HTTP: %d | HTTPS: %d | SSL: %s | porta WSL: %d\n", cfg.DefaultMode, cfg.WindowsPort, cfg.HTTPSPort, sslState(cfg.TLSEnabled), cfg.WSLPort)
	if err := service.WindowsCaddy.Available(ctx); err == nil {
		fmt.Println("Caddy Windows: disponível")
	} else {
		fmt.Println("Caddy Windows: ausente")
	}
	if err := service.WSLCaddy.Available(ctx); err == nil {
		fmt.Println("Caddy WSL: disponível")
	} else {
		fmt.Println("Caddy WSL: ausente")
	}
	if versions, versionErr := service.PHPVersions(ctx); versionErr == nil {
		labels := make([]string, 0, len(versions))
		for _, version := range versions {
			status := "detectada"
			if version.Configured {
				status = "configurada"
			}
			if version.Configured && version.Installed {
				status = "ativa"
			}
			labels = append(labels, version.Version+" ("+status+")")
		}
		fmt.Printf("PHP padrão: %s | versões: %s\n", cfg.PHPDefaultVersion, strings.Join(labels, ", "))
	} else {
		fmt.Printf("PHP: indisponível (%s)\n", versionErr)
	}
	fmt.Printf("Projetos registrados: %d | parks: %d\n", len(cfg.Projects), len(cfg.Parks))
	return printProjects(service, "", false)
}

func printWarnings(warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "[aviso] %s\n", warning)
	}
}

func printUsage() {
	fmt.Print(`DevLAN — publicar projetos Laravel do WSL na rede local

Uso:
  devlan [--data-dir DIR] COMANDO [ARGUMENTOS]

Fundação e registro:
  install [--no-firewall] [--windows-port PORT]
                              inicializa arquivos gerenciados (Administrador*)
  uninstall                  remove arquivos gerenciados, preserva projetos (Administrador*)
  link NAME PATH             registra um projeto Laravel
  unlink NAME                remove registro e rota
  links [FILTRO] [--json]    lista projetos registrados e descobertos
  park PATH                  registra uma pasta de projetos
  unpark PATH                remove uma pasta estacionada
  parked                     lista pastas estacionadas

Operação:
  status                     mostra componentes, projetos e URLs
  reload                     valida/aplica configurações e recarrega Caddy
  trust                      instala e confia na CA interna do Caddy (Administrador*)
  secure NAME|PATH           ativa HTTPS para um projeto (Administrador*)
  unsecure NAME|PATH         desativa HTTPS para um projeto
  doctor [NAME]              diagnostica host, runtime e projeto
  logs [COMPONENT]           mostra logs gerenciados
  open [NAME]                abre URL ou mostra o dashboard textual
  mode default php           define o modo global
  mode NAME php|inherit      sobrescreve ou restaura herança

PHP:
  php list                   lista versões, extensões e estado
  php install VERSION        instala PHP-FPM, Composer e extensões
  php remove VERSION         remove uma versão não usada
  php use default VERSION    define a versão PHP global
  php use NAME VERSION       sobrescreve a versão do projeto
  php extensions VERSION ... define extensões da versão
  php pool default|VERSION   configura limites e timeout do pool
  php pool NAME shared|isolated escolhe pool por projeto
  php preset NAME PRESET     usa laravel, symfony ou generic
  php info [NAME]            mostra página sanitizada de informações
  composer VERSION|NAME      executa Composer com a versão escolhida
  composer config default|NAME ENV  define ambiente do Composer

Variável de ambiente:
  DEVLAN_HOME                diretório de dados (padrão: %LOCALAPPDATA%\DevLAN)

* Administrador é necessário para criar/remover regras de firewall e confiar
  na CA interna. Os demais comandos funcionam normalmente sem elevação.
`)
}

func containsHelp(args []string) bool {
	for _, argument := range args {
		if argument == "-h" || argument == "--help" {
			return true
		}
	}
	return false
}

func printCommandUsage(command string) {
	if command == "php" {
		printPHPUsage()
		return
	}
	if command == "composer" {
		printComposerUsage()
		return
	}

	usages := map[string]string{
		"install":   "uso: devlan install [--no-firewall] [--windows-port PORT]",
		"uninstall": "uso: devlan uninstall",
		"link":      "uso: devlan link NAME PATH",
		"unlink":    "uso: devlan unlink NAME",
		"links":     "uso: devlan links [FILTRO] [--json]",
		"park":      "uso: devlan park PATH",
		"unpark":    "uso: devlan unpark PATH",
		"parked":    "uso: devlan parked",
		"status":    "uso: devlan status",
		"reload":    "uso: devlan reload",
		"trust":     "uso: devlan trust",
		"secure":    "uso: devlan secure NAME|PATH",
		"unsecure":  "uso: devlan unsecure NAME|PATH",
		"doctor":    "uso: devlan doctor [NAME]",
		"logs":      "uso: devlan logs [COMPONENT]",
		"open":      "uso: devlan open [NAME]",
		"mode":      "uso: devlan mode default MODE | devlan mode NAME MODE|inherit",
	}
	if usage, ok := usages[command]; ok {
		fmt.Printf("%s\n\nOpções:\n  -h, --help    mostra esta ajuda\n", usage)
		switch command {
		case "install", "uninstall":
			fmt.Println("\nAdministrador: necessário para criar ou remover a regra de firewall.")
		case "secure":
			fmt.Println("\nAdministrador: necessário na primeira ativação para liberar a porta HTTPS no firewall e confiar na CA interna.")
		case "trust":
			fmt.Println("\nAdministrador: necessário para instalar e confiar na CA raiz do Caddy no sistema.")
		}
		return
	}
	printUsage()
}

func printPHPUsage() {
	fmt.Print(`Uso: devlan php SUBCOMANDO [ARGUMENTOS]

Gerenciamento de versões PHP-FPM no WSL e seleção por projeto.

Subcomandos:
  list
      Lista a versão padrão, versões detectadas/instaladas e extensões.

  install VERSION [--extensions EXT1,EXT2]
      Instala e registra uma versão do PHP-FPM no WSL.
      Se --extensions não for informado, usa a lista padrão do DevLAN.

  remove VERSION
      Remove uma versão registrada. Não remove a versão padrão nem uma versão
      que ainda esteja selecionada por um projeto.
      Alias: uninstall.

  use default VERSION
      Define a versão PHP global para projetos sem uma sobrescrita.

  use NAME VERSION
      Define a versão PHP somente para o projeto NAME.

  use NAME inherit
      Remove a sobrescrita de NAME e volta a herdar a versão global.

  extensions VERSION
      Mostra as extensões configuradas para VERSION, uma por linha.

  extensions VERSION EXT1 EXT2 ...
      Substitui a lista de extensões de VERSION.
      Extensões também podem ser separadas por vírgulas.
      Alias: ext.

  pool default [OPÇÕES]
      Configura o pool global compartilhado.

  pool VERSION [OPÇÕES]
      Configura o pool compartilhado da versão VERSION.

  pool NAME shared|isolated
      Escolhe o pool compartilhado ou isolado para o projeto NAME.

  preset NAME laravel|symfony|generic|inherit
      Define o preset do projeto. inherit remove a sobrescrita do projeto.

  info [NAME]
      Imprime uma página HTML sanitizada com as informações do PHP do projeto
      ou, sem NAME, da configuração global.

Opções de pool:
  --max-children N       máximo de workers PHP-FPM
  --idle-timeout DURAÇÃO tempo ocioso antes de encerrar workers
  --max-requests N       requisições atendidas por worker antes de reciclar

Exemplos:
  devlan php list
  devlan php install 8.3 --extensions mbstring,xml,curl
  devlan php use default 8.3
  devlan php use loja 8.2
  devlan php use loja inherit
  devlan php pool default --max-children 10 --idle-timeout 10s
  devlan php pool loja isolated
  devlan php preset loja laravel
  devlan php info loja

Ajuda:
  devlan php -h
  devlan php SUBCOMANDO -h
`)
}

func printComposerUsage() {
	fmt.Print(`Uso: devlan composer SELETOR [OPÇÕES] -- ARGUMENTOS
     devlan composer config default|NAME auto|system|per-version

Executa o Composer dentro do WSL usando a versão PHP selecionada.

SELETOR:
  VERSION                 usa essa versão PHP (ex.: 8.3)
  NAME                    usa a versão efetiva do projeto NAME

Opções:
  --environment ENV       auto, system ou per-version
  --                      separa as opções do DevLAN dos argumentos do Composer

Ambientes do Composer:
  auto                    escolhe automaticamente o ambiente por versão
  system                  usa o Composer global do WSL
  per-version             usa o Composer associado à versão PHP

Configuração:
  devlan composer config default auto
  devlan composer config loja per-version

Exemplos:
  devlan composer 8.3 -- install
  devlan composer loja -- update
  devlan composer loja --environment per-version -- install laravel/framework

Ajuda:
  devlan composer -h
`)
}
