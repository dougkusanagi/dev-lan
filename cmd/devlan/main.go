package main

import (
	"context"
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
		if err := service.Uninstall(ctx); err != nil {
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
		if len(args) != 0 {
			return fmt.Errorf("uso: devlan links")
		}
		return printProjects(service)

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
			return printProjects(service)
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

func printProjects(service *app.App) error {
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
		fmt.Println("Nenhum projeto registrado.")
		return nil
	}
	rows := make([]projectRow, 0, len(effective.Projects))
	for _, project := range effective.Projects {
		resolved, err := effective.Resolve(project.Name)
		if err != nil {
			return err
		}
		url := resolved.URL(host, cfg.WindowsPort, cfg.HTTPSPort, effective.SecureProject(project))
		rows = append(rows, projectRow{
			Name: project.Name, Mode: string(resolved.Mode), Source: string(resolved.Source), SSL: sslState(cfg.SecureProject(project)), URL: url, Path: project.Path,
		})
	}
	return writeProjectTable(os.Stdout, rows)
}

type projectRow struct {
	Name   string
	Mode   string
	Source string
	SSL    string
	URL    string
	Path   string
}

func writeProjectTable(output io.Writer, rows []projectRow) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "PROJETO\tMODO\tORIGEM\tSSL\tURL\tCAMINHO"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "-------\t----\t------\t---\t---\t-------"); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", row.Name, row.Mode, row.Source, row.SSL, row.URL, row.Path); err != nil {
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
	fmt.Printf("Projetos registrados: %d | parks: %d\n", len(cfg.Projects), len(cfg.Parks))
	return printProjects(service)
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
  links                      lista projetos registrados
  park PATH                  registra uma pasta de projetos
  unpark PATH                remove uma pasta estacionada
  parked                     lista pastas estacionadas

Operação:
  status                     mostra componentes, projetos e URLs
  reload                     valida/aplica configurações e recarrega Caddy
  secure NAME|PATH           ativa HTTPS para um projeto (Administrador*)
  unsecure NAME|PATH         desativa HTTPS para um projeto
  doctor [NAME]              diagnostica host, runtime e projeto
  logs [COMPONENT]           mostra logs gerenciados
  open [NAME]                abre URL ou mostra o dashboard textual
  mode default php           define o padrão do MVP
  mode NAME php|inherit      sobrescreve ou restaura herança

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
	usages := map[string]string{
		"install":   "uso: devlan install [--no-firewall] [--windows-port PORT]",
		"uninstall": "uso: devlan uninstall",
		"link":      "uso: devlan link NAME PATH",
		"unlink":    "uso: devlan unlink NAME",
		"links":     "uso: devlan links",
		"park":      "uso: devlan park PATH",
		"unpark":    "uso: devlan unpark PATH",
		"parked":    "uso: devlan parked",
		"status":    "uso: devlan status",
		"reload":    "uso: devlan reload",
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
		}
		return
	}
	printUsage()
}
