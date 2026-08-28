package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/application"
	"github.com/dougkusanagi/dev-lan/internal/desktop"
	backgroundservice "github.com/dougkusanagi/dev-lan/internal/service"
	"github.com/dougkusanagi/dev-lan/internal/startup"
)

const version = "0.0.1"

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

	command := commandArgs[0]
	args = commandArgs[1:]
	if containsHelp(args) {
		printCommandUsage(command)
		return nil
	}
	// The Linux artifact installed in WSL is deliberately a thin client. It
	// never opens a second store or runs a second controller; it forwards the
	// supported operational commands to the authenticated Windows API.
	if runtime.GOOS == "linux" {
		if command == "topology" && len(args) > 0 && args[0] == "migrate" {
			return fmt.Errorf("a migração da topologia deve ser iniciada pelo controlador Windows")
		}
		return runWSLClient(context.Background(), dataDir, command, args)
	}
	service := app.New(dataDir)
	commands := application.NewCommands(service, service)
	queries := application.NewQueries(service)
	var ctx context.Context
	var cancel context.CancelFunc
	if command == "gui" && len(args) > 0 && (args[0] == "--foreground" || args[0] == "-f") {
		ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt)
	} else if (command == "api" && (len(args) == 0 || args[0] == "serve")) ||
		(command == "service" && len(args) > 0 && args[0] == "run") {
		ctx = context.Background()
		cancel = func() {}
	} else {
		ctx, cancel = context.WithTimeout(context.Background(), cliCommandTimeout(command, args))
	}
	defer cancel()

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
		result, err := commands.InstallWithPort(ctx, configureFirewall, windowsPort)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("DevLAN inicializado em %s\n", dataDir)
		fmt.Println("Componentes externos são verificados por `devlan doctor`.")
		return nil

	case "uninstall":
		uninstallOptions := application.UninstallOptions{}
		asJSON := false
		for _, argument := range args {
			switch argument {
			case "--dry-run":
				uninstallOptions.DryRun = true
			case "--keep-data":
				uninstallOptions.KeepData = true
			case "--keep-dependencies":
				uninstallOptions.KeepDependencies = true
			case "--purge":
				uninstallOptions.Purge = true
			case "--yes":
				uninstallOptions.Yes = true
			case "--json":
				asJSON = true
			default:
				return fmt.Errorf("uso: devlan uninstall [--dry-run] [--keep-data] [--keep-dependencies] [--purge --yes] [--json]")
			}
		}
		if err := uninstallOptions.Validate(); err != nil {
			return err
		}
		if !uninstallOptions.DryRun && runtime.GOOS == "windows" {
			manager := backgroundservice.NewManager()
			if status, statusErr := manager.Status(ctx); statusErr == nil && status.Installed {
				if removeErr := manager.Remove(ctx); removeErr != nil {
					fmt.Fprintf(os.Stderr, "[aviso] não foi possível remover o serviço DevLAN: %v\n", removeErr)
				}
			}
			if startupErr := startup.Disable(ctx); startupErr != nil {
				fmt.Fprintf(os.Stderr, "[aviso] não foi possível remover a inicialização automática: %v\n", startupErr)
			}
		}
		if !uninstallOptions.DryRun && runtime.GOOS == "windows" {
			if desktopErr := desktop.Uninstall(ctx, dataDir); desktopErr != nil {
				fmt.Fprintf(os.Stderr, "[aviso] não foi possível remover a integração desktop: %v\n", desktopErr)
			}
		}
		result, err := commands.UninstallWithOptions(ctx, uninstallOptions)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(os.Stdout).Encode(result)
		}
		printUninstallPlan(result.Plan, uninstallOptions.DryRun)
		if result.Completed {
			fmt.Println("DevLAN removido; diretórios dos projetos foram preservados.")
		} else if result.Plan.Pending && len(result.Plan.Warnings) > 0 {
			fmt.Println("DevLAN removido; a aplicação de configurações WSL ainda está pendente (consulte os avisos).")
		} else {
			fmt.Println("DevLAN removido parcialmente; consulte os avisos e execute novamente após corrigir as etapas pendentes.")
		}
		return nil

	case "link":
		if len(args) != 2 {
			return fmt.Errorf("uso: devlan link NAME PATH")
		}
		project, result, err := commands.LinkProject(ctx, application.LinkProjectCommand{Name: args[0], Path: args[1]})
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
		project, result, err := commands.UnlinkProject(ctx, application.UnlinkProjectCommand{Name: args[0]})
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
		return printProjects(ctx, queries, filter, asJSON)

	case "park":
		if len(args) == 2 && args[0] == "ignore" {
			result, err := commands.IgnoreProject(ctx, application.IgnoreProjectCommand{Selector: args[1]})
			printWarnings(result.Warnings)
			if err != nil {
				return err
			}
			fmt.Printf("Projeto %s ocultado da lista de projetos estacionados.\n", args[1])
			return nil
		}
		if len(args) == 2 && args[0] == "unignore" {
			result, err := commands.UnignoreProject(ctx, application.UnignoreProjectCommand{Path: args[1]})
			printWarnings(result.Warnings)
			if err != nil {
				return err
			}
			fmt.Printf("Projeto %s voltou à lista de projetos estacionados.\n", args[1])
			return nil
		}
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan park PATH | devlan park ignore NAME|PATH | devlan park unignore PATH")
		}
		park, result, err := commands.ParkDirectory(ctx, application.ParkDirectoryCommand{Path: args[0]})
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
		park, result, err := commands.UnparkDirectory(ctx, application.UnparkDirectoryCommand{Path: args[0]})
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
		cfg, err := queries.Config(ctx)
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
		return runMode(ctx, commands, args)

	case "php":
		return runPHP(ctx, commands, queries, args)

	case "composer":
		return runComposer(ctx, commands, args)

	case "status":
		if len(args) != 0 {
			return fmt.Errorf("uso: devlan status")
		}
		return printStatus(ctx, queries, dataDir)

	case "topology":
		return runTopology(ctx, commands, queries, args)

	case "reload":
		if len(args) != 0 {
			return fmt.Errorf("uso: devlan reload")
		}
		result, err := commands.Reload(ctx)
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
		if err := commands.Trust(ctx); err != nil {
			return fmt.Errorf("não foi possível confiar na CA local automaticamente (%w); execute `devlan trust` em PowerShell como Administrador", err)
		}
		fmt.Println("Certificado raiz local do Caddy instalado e confiado no sistema.")
		return nil

	case "secure", "unsecure":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan %s NAME|PATH", command)
		}
		enabled := command == "secure"
		result, projectName, err := commands.SetProjectTLS(ctx, args[0], enabled)
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
		checks, err := queries.Doctor(ctx, name)
		if err != nil {
			return err
		}
		for _, check := range checks {
			fmt.Printf("[%s] %-20s %s\n", check.Status, check.Name, check.Detail)
		}
		return nil

	case "start":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan start NAME")
		}
		if err := commands.StartDev(ctx, args[0]); err != nil {
			return err
		}
		fmt.Printf("Servidor dev iniciado para %s.\n", args[0])
		return nil

	case "stop":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan stop NAME")
		}
		if err := commands.StopDev(ctx, args[0]); err != nil {
			return err
		}
		fmt.Printf("Servidor dev parado para %s.\n", args[0])
		return nil

	case "restart":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan restart NAME")
		}
		if err := commands.RestartDev(ctx, args[0]); err != nil {
			return err
		}
		fmt.Printf("Servidor dev reiniciado para %s.\n", args[0])
		return nil

	case "build":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan build NAME")
		}
		out, err := commands.BuildProject(ctx, args[0])
		fmt.Print(out)
		return err

	case "deps":
		if len(args) < 1 {
			return fmt.Errorf("uso: devlan deps install NAME | devlan deps NAME")
		}
		name := args[0]
		if args[0] == "install" {
			if len(args) != 2 {
				return fmt.Errorf("uso: devlan deps install NAME")
			}
			name = args[1]
		}
		out, err := commands.InstallDeps(ctx, name)
		fmt.Print(out)
		return err

	case "static":
		if len(args) < 1 || len(args) > 2 {
			return fmt.Errorf("uso: devlan static NAME [DIR]")
		}
		dir := ""
		if len(args) == 2 {
			dir = args[1]
		}
		res, err := commands.SetProjectStaticDir(ctx, args[0], dir)
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Diretório estático do projeto %s configurado.\n", args[0])
		return nil

	case "dev":
		return runDevCommand(ctx, commands, queries, args)

	case "logs":
		if len(args) > 1 {
			return fmt.Errorf("uso: devlan logs [COMPONENT]")
		}
		component := ""
		if len(args) == 1 {
			component = args[0]
		}
		logs, err := queries.Logs(component)
		if err != nil {
			// check if it's a dev project log
			devLog, devErr := queries.ProjectDevLogs(ctx, component, 100)
			if devErr == nil && devLog != "" {
				fmt.Print(devLog)
				return nil
			}
			return err
		}
		fmt.Print(logs)
		return nil

	case "open":
		if len(args) > 1 {
			return fmt.Errorf("uso: devlan open [NAME]")
		}
		if len(args) == 0 {
			return printProjects(ctx, queries, "", false)
		}
		url, err := commands.Open(ctx, args[0])
		fmt.Println(url)
		if err != nil {
			return fmt.Errorf("URL calculada, mas não foi possível abrir o navegador: %w", err)
		}
		return nil

	case "route":
		return runRoute(ctx, commands, queries, args)

	case "expose":
		return runExpose(ctx, commands, args)

	case "unexpose":
		return runUnexpose(ctx, commands, args)

	case "allowlist":
		return runAllowlist(ctx, commands, queries, args)

	case "auth":
		return runAuth(ctx, commands, args)

	case "ca":
		return runCA(ctx, commands, queries, args)

	case "gui":
		return runGUI(ctx, service, commands, queries, dataDir, args)

	case "desktop":
		return runDesktop(ctx, dataDir, args)

	case "security":
		return runSecurity(ctx, commands, queries, args)

	case "config":
		return runConfig(ctx, commands, queries, args)

	case "diagnostic":
		if len(args) > 1 {
			return fmt.Errorf("uso: devlan diagnostic [PATH]")
		}
		target := ""
		if len(args) == 1 {
			target = args[0]
		}
		path, err := commands.DiagnosticBundle(ctx, target)
		if err != nil {
			return err
		}
		fmt.Printf("Diagnóstico exportado para %s\n", path)
		return nil

	case "api":
		return runAPI(ctx, service, commands, queries, args)

	case "service":
		return runBackgroundService(ctx, dataDir, args)

	case "startup":
		return runStartup(ctx, dataDir, args)

	case "telemetry":
		return runTelemetry(ctx, commands, queries, args)

	case "update":
		return runUpdate(ctx, args)

	default:
		return fmt.Errorf("comando desconhecido %q; use `devlan help`", command)
	}
}

func cliCommandTimeout(command string, args []string) time.Duration {
	// A topology migration deliberately performs wsl --shutdown and then boots
	// the VM, systemd and Caddy again. A cold WSL start can consume most of the
	// normal command budget on otherwise healthy machines.
	if command == "topology" {
		for _, arg := range args {
			if arg == "migrate" {
				return 3 * time.Minute
			}
		}
	}
	if command == "uninstall" {
		return 5 * time.Minute
	}
	return 45 * time.Second
}
