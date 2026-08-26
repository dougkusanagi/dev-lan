package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	localapi "github.com/dougkusanagi/dev-lan/internal/api"
	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/config"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/gui"
	"github.com/dougkusanagi/dev-lan/internal/platform"
	backgroundservice "github.com/dougkusanagi/dev-lan/internal/service"
	"github.com/dougkusanagi/dev-lan/internal/startup"
	devlanupdate "github.com/dougkusanagi/dev-lan/internal/update"
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
		return runWSLClient(context.Background(), dataDir, command, args)
	}
	service := app.New(dataDir)
	var ctx context.Context
	var cancel context.CancelFunc
	if (command == "api" && len(args) > 0 && args[0] == "serve") ||
		(command == "service" && len(args) > 0 && args[0] == "run") {
		ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt)
	} else {
		ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
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
		if runtime.GOOS == "windows" {
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
		if len(args) == 2 && args[0] == "ignore" {
			result, err := service.IgnoreProject(ctx, args[1])
			printWarnings(result.Warnings)
			if err != nil {
				return err
			}
			fmt.Printf("Projeto %s ocultado da lista de projetos estacionados.\n", args[1])
			return nil
		}
		if len(args) == 2 && args[0] == "unignore" {
			result, err := service.UnignoreProject(ctx, args[1])
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

	case "start":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan start NAME")
		}
		if err := service.StartDev(ctx, args[0]); err != nil {
			return err
		}
		fmt.Printf("Servidor dev iniciado para %s.\n", args[0])
		return nil

	case "stop":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan stop NAME")
		}
		if err := service.StopDev(ctx, args[0]); err != nil {
			return err
		}
		fmt.Printf("Servidor dev parado para %s.\n", args[0])
		return nil

	case "restart":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan restart NAME")
		}
		if err := service.RestartDev(ctx, args[0]); err != nil {
			return err
		}
		fmt.Printf("Servidor dev reiniciado para %s.\n", args[0])
		return nil

	case "build":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan build NAME")
		}
		out, err := service.BuildProject(ctx, args[0])
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
		out, err := service.InstallDeps(ctx, name)
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
		res, err := service.SetProjectStaticDir(ctx, args[0], dir)
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Diretório estático do projeto %s configurado.\n", args[0])
		return nil

	case "dev":
		return runDevCommand(ctx, service, args)

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
			// check if it's a dev project log
			devLog, devErr := service.ProjectDevLogs(ctx, component, 100)
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
			return printProjects(service, "", false)
		}
		url, err := service.Open(ctx, args[0])
		fmt.Println(url)
		if err != nil {
			return fmt.Errorf("URL calculada, mas não foi possível abrir o navegador: %w", err)
		}
		return nil

	case "route":
		return runRoute(ctx, service, args)

	case "expose":
		return runExpose(ctx, service, args)

	case "unexpose":
		return runUnexpose(ctx, service, args)

	case "allowlist":
		return runAllowlist(ctx, service, args)

	case "auth":
		return runAuth(ctx, service, args)

	case "ca":
		return runCA(ctx, service, args)

	case "gui":
		return gui.Launch(service)

	case "security":
		return runSecurity(ctx, service, args)

	case "config":
		return runConfig(ctx, service, args)

	case "diagnostic":
		if len(args) > 1 {
			return fmt.Errorf("uso: devlan diagnostic [PATH]")
		}
		target := ""
		if len(args) == 1 {
			target = args[0]
		}
		path, err := service.DiagnosticBundle(ctx, target)
		if err != nil {
			return err
		}
		fmt.Printf("Diagnóstico exportado para %s\n", path)
		return nil

	case "api":
		return runAPI(ctx, service, args)

	case "service":
		return runBackgroundService(ctx, dataDir, args)

	case "startup":
		return runStartup(ctx, dataDir, args)

	case "telemetry":
		return runTelemetry(ctx, service, args)

	case "update":
		return runUpdate(ctx, args)

	default:
		return fmt.Errorf("comando desconhecido %q; use `devlan help`", command)
	}
}

func runConfig(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan config export [PATH] | devlan config import PATH")
	}
	switch args[0] {
	case "export":
		if len(args) > 2 {
			return fmt.Errorf("uso: devlan config export [PATH]")
		}
		data, err := service.ExportConfig()
		if err != nil {
			return err
		}
		if len(args) == 1 || args[1] == "-" {
			_, err = os.Stdout.Write(data)
			return err
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Clean(args[1])), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(args[1], data, 0o600); err != nil {
			return fmt.Errorf("gravar exportação: %w", err)
		}
		fmt.Printf("Configuração exportada para %s (sem credenciais)\n", args[1])
		return nil
	case "import":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" || args[1] == "-" {
			return fmt.Errorf("uso: devlan config import PATH")
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			return fmt.Errorf("ler importação: %w", err)
		}
		result, err := service.ImportConfig(ctx, data)
		printWarnings(result.Warnings)
		if err != nil {
			return err
		}
		fmt.Println("Configuração importada, validada e aplicada.")
		return nil
	default:
		return fmt.Errorf("subcomando config desconhecido: %s", args[0])
	}
}

func runAPI(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 || args[0] == "serve" {
		if len(args) > 1 {
			return fmt.Errorf("uso: devlan api serve")
		}
		server := localapi.New(service)
		endpoint, err := server.Start()
		if err != nil {
			return err
		}
		fmt.Printf("API local autenticada em %s\n", endpoint.Address)
		<-ctx.Done()
		return server.Close(context.Background())
	}
	if args[0] == "status" && len(args) == 1 {
		client := localapi.Client{Store: service.Store}
		response, err := client.Do(ctx, http.MethodGet, "/v1/health", nil)
		if err != nil {
			return fmt.Errorf("API local indisponível: %w", err)
		}
		defer response.Body.Close()
		data, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			return readErr
		}
		fmt.Print(string(data))
		if response.StatusCode >= 400 {
			return fmt.Errorf("API local respondeu HTTP %d", response.StatusCode)
		}
		return nil
	}
	return fmt.Errorf("uso: devlan api serve | devlan api status")
}

func runBackgroundService(ctx context.Context, dataDir string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan service install|remove|start|stop|status|run")
	}
	manager := backgroundservice.NewManager()
	switch args[0] {
	case "run":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan service run")
		}
		return backgroundservice.Run(ctx, dataDir)
	case "install":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan service install")
		}
		options, err := backgroundservice.DefaultOptions(dataDir)
		if err != nil {
			return err
		}
		if err := manager.Install(ctx, options); err != nil {
			return err
		}
		fmt.Println("Serviço DevLAN instalado para iniciar automaticamente no boot.")
		return nil
	case "remove":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan service remove")
		}
		if err := manager.Remove(ctx); err != nil {
			return err
		}
		fmt.Println("Serviço DevLAN removido.")
		return nil
	case "start", "stop":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan service %s", args[0])
		}
		var err error
		if args[0] == "start" {
			err = manager.Start(ctx)
		} else {
			err = manager.Stop(ctx)
		}
		if err != nil {
			return err
		}
		fmt.Printf("Serviço DevLAN: %s.\n", args[0])
		return nil
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan service status")
		}
		status, err := manager.Status(ctx)
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(status, "", "  ")
		fmt.Println(string(data))
		return nil
	default:
		return fmt.Errorf("subcomando service desconhecido: %s", args[0])
	}
}

func runStartup(ctx context.Context, dataDir string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan startup enable [gui|service] | disable | status")
	}
	switch args[0] {
	case "enable":
		if len(args) > 2 {
			return fmt.Errorf("uso: devlan startup enable [gui|service]")
		}
		mode := startup.ModeGUI
		if len(args) == 2 {
			parsed, err := startup.ParseMode(args[1])
			if err != nil {
				return err
			}
			mode = parsed
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		if err := startup.Enable(ctx, executable, dataDir, mode); err != nil {
			return err
		}
		fmt.Printf("Inicialização automática habilitada (%s).\n", mode)
		return nil
	case "disable":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan startup disable")
		}
		if err := startup.Disable(ctx); err != nil {
			return err
		}
		fmt.Println("Inicialização automática desabilitada.")
		return nil
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan startup status")
		}
		state, err := startup.Status(ctx)
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(state, "", "  ")
		fmt.Println(string(data))
		return nil
	default:
		return fmt.Errorf("subcomando startup desconhecido: %s", args[0])
	}
}

func runTelemetry(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan telemetry status|enable ENDPOINT|disable|send")
	}
	store := service.Telemetry
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan telemetry status")
		}
		consent, err := store.Load()
		if err != nil {
			return err
		}
		queued, err := store.QueueSize()
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(map[string]any{
			"enabled":  consent.Enabled,
			"endpoint": consent.Endpoint,
			"queued":   queued,
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	case "enable":
		if len(args) != 2 {
			return fmt.Errorf("uso: devlan telemetry enable ENDPOINT")
		}
		if err := store.SetConsent(true, args[1]); err != nil {
			return err
		}
		fmt.Println("Telemetria habilitada com consentimento explícito; o envio continua manual (`devlan telemetry send`).")
		return nil
	case "disable":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan telemetry disable")
		}
		if err := store.SetConsent(false, ""); err != nil {
			return err
		}
		fmt.Println("Telemetria desabilitada e fila local removida.")
		return nil
	case "send":
		if len(args) != 1 {
			return fmt.Errorf("uso: devlan telemetry send")
		}
		count, err := store.Send(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("%d evento(s) de telemetria enviado(s).\n", count)
		return nil
	default:
		return fmt.Errorf("subcomando telemetry desconhecido: %s", args[0])
	}
}

func runUpdate(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan update check CHANNEL [MANIFEST_URL] | devlan update download CHANNEL MANIFEST_URL PATH")
	}
	if len(args) < 2 {
		return fmt.Errorf("canal obrigatório: stable ou preview")
	}
	channel, err := devlanupdate.ParseChannel(args[1])
	if err != nil {
		return err
	}
	switch args[0] {
	case "check":
		if len(args) > 3 {
			return fmt.Errorf("uso: devlan update check CHANNEL [MANIFEST_URL]")
		}
		manifestURL := ""
		if len(args) == 3 {
			manifestURL = args[2]
		} else {
			manifestURL = os.Getenv("DEVLAN_UPDATE_MANIFEST_" + strings.ToUpper(string(channel)) + "_URL")
		}
		if strings.TrimSpace(manifestURL) == "" {
			return fmt.Errorf("informe MANIFEST_URL ou DEVLAN_UPDATE_MANIFEST_%s_URL", strings.ToUpper(string(channel)))
		}
		manifest, err := devlanupdate.FetchManifest(ctx, nil, manifestURL, channel)
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(map[string]any{
			"channel":      channel,
			"current":      version,
			"available":    manifest.Version,
			"update":       devlanupdate.IsNewer(version, manifest.Version),
			"sha256":       manifest.SHA256,
			"artifact_url": manifest.URL,
		}, "", "  ")
		fmt.Println(string(data))
		return nil
	case "download":
		if len(args) != 4 {
			return fmt.Errorf("uso: devlan update download CHANNEL MANIFEST_URL PATH")
		}
		manifest, err := devlanupdate.FetchManifest(ctx, nil, args[2], channel)
		if err != nil {
			return err
		}
		if err := devlanupdate.DownloadVerified(ctx, nil, manifest, channel, args[3]); err != nil {
			return err
		}
		fmt.Printf("Update %s verificado por SHA-256 e preparado em %s.\n", manifest.Version, args[3])
		return nil
	default:
		return fmt.Errorf("subcomando update desconhecido: %s", args[0])
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
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/etc/devlan/windows-data-dir"); err == nil {
			if configured := strings.TrimSpace(string(data)); configured != "" {
				return filepath.Clean(configured)
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".devlan")
	}
	return ".devlan"
}

func runWSLClient(ctx context.Context, dataDir, command string, args []string) error {
	allowed := map[string]bool{
		"link": true, "unlink": true, "park": true, "unpark": true,
		"links": true, "status": true, "reload": true, "doctor": true, "open": true,
	}
	if !allowed[command] {
		return fmt.Errorf("comando %q ainda não está disponível no cliente WSL; use o controlador Windows", command)
	}
	client := localapi.Client{Store: configStore(dataDir)}
	requestContext, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	payload, err := client.Command(requestContext, command, args)
	if err != nil {
		return fmt.Errorf("controlador Windows indisponível: %w (inicie `devlan service start` ou a UI)", err)
	}
	if message, ok := payload["message"].(string); ok && message != "" {
		fmt.Println(message)
	}
	if command == "links" || command == "status" || command == "doctor" {
		if command == "links" {
			if projects, ok := payload["projects"]; ok {
				data, _ := json.MarshalIndent(projects, "", "  ")
				fmt.Println(string(data))
			}
		} else if command == "status" {
			if status, ok := payload["status"]; ok {
				data, _ := json.MarshalIndent(status, "", "  ")
				fmt.Println(string(data))
			}
		} else if checks, ok := payload["checks"]; ok {
			data, _ := json.MarshalIndent(checks, "", "  ")
			fmt.Println(string(data))
		}
	}
	return nil
}

// Kept as a helper to make the WSL transport explicit and easy to substitute
// in tests without changing the Windows application construction path.
func configStore(dataDir string) config.Store { return config.NewStore(dataDir) }

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

func runDevCommand(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan dev NAME [start|stop|restart|status|logs|--port PORT|--command CMD|--pm PM]")
	}
	name := args[0]
	if len(args) == 1 {
		return service.StartDev(ctx, name)
	}
	switch args[1] {
	case "start":
		return service.StartDev(ctx, name)
	case "stop":
		return service.StopDev(ctx, name)
	case "restart":
		return service.RestartDev(ctx, name)
	case "status":
		st, err := service.DevStatus(ctx, name)
		if err != nil {
			return err
		}
		fmt.Printf("Projeto: %s | Porta: %d | Estado: %s | PID: %d\n", st.ProjectName, st.Port, st.State, st.PID)
		return nil
	case "logs":
		logs, err := service.ProjectDevLogs(ctx, name, 100)
		if err != nil {
			return err
		}
		fmt.Print(logs)
		return nil
	default:
		// parse flags
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--port":
				if i+1 >= len(args) {
					return fmt.Errorf("--port exige um número de porta")
				}
				port, err := strconv.Atoi(args[i+1])
				if err != nil || port < 1024 || port > 65535 {
					return fmt.Errorf("porta dev inválida: %s", args[i+1])
				}
				res, err := service.SetProjectDevPort(ctx, name, port)
				printWarnings(res.Warnings)
				if err != nil {
					return err
				}
				i++
			case "--command", "--cmd":
				if i+1 >= len(args) {
					return fmt.Errorf("--command exige uma string de comando")
				}
				res, err := service.SetProjectDevCommand(ctx, name, args[i+1])
				printWarnings(res.Warnings)
				if err != nil {
					return err
				}
				i++
			case "--pm":
				if i+1 >= len(args) {
					return fmt.Errorf("--pm exige um gerenciador (npm, pnpm, yarn, bun)")
				}
				res, err := service.SetProjectPackageManager(ctx, name, args[i+1])
				printWarnings(res.Warnings)
				if err != nil {
					return err
				}
				i++
			default:
				return fmt.Errorf("opção desconhecida: %s", args[i])
			}
		}
		fmt.Printf("Configurações de dev do projeto %s atualizadas.\n", name)
		return nil
	}
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

		runtimeStr := "-"
		typeStr := string(resolved.Mode)
		switch resolved.Mode {
		case domain.ModePHP:
			runtimeStr = effective.EffectivePHPVersion(project)
			typeStr = string(effective.PHPProjectPreset(project))
		case domain.ModeStatic:
			staticDir := "root"
			if project.StaticDir != nil && *project.StaticDir != "" {
				staticDir = *project.StaticDir
			}
			runtimeStr = "-"
			typeStr = "static (" + staticDir + ")"
		case domain.ModeDev:
			fw := "generic"
			if project.DevFramework != nil {
				fw = *project.DevFramework
			}
			port := effective.DevPort(project)
			runtimeStr = fmt.Sprintf("%s :%d", fw, port)
			pm := effective.PackageManager(project)
			typeStr = "dev (" + pm + ")"
		}

		rows = append(rows, projectRow{
			Name:    project.Name,
			Mode:    string(resolved.Mode),
			Runtime: runtimeStr,
			Type:    typeStr,
			Source:  string(resolved.Source),
			SSL:     sslState(cfg.SecureProject(project)),
			URL:     url,
			Path:    project.Path,
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
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	Runtime string `json:"runtime"`
	Type    string `json:"type"`
	Source  string `json:"source"`
	SSL     string `json:"ssl"`
	URL     string `json:"url"`
	Path    string `json:"path"`
}

func writeProjectTable(output io.Writer, rows []projectRow) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "PROJETO\tMODO\tRUNTIME\tTIPO\tORIGEM\tSSL\tURL\tCAMINHO"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "-------\t----\t-------\t----\t------\t---\t---\t-------"); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.Name, row.Mode, row.Runtime, row.Type, row.Source, row.SSL, row.URL, row.Path); err != nil {
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
	fmt.Print(`DevLAN — publicar projetos PHP, JavaScript e estáticos do WSL na rede local

Uso:
  devlan [--data-dir DIR] COMANDO [ARGUMENTOS]

Fundação e registro:
  install [--no-firewall] [--windows-port PORT]
                              inicializa arquivos gerenciados (Administrador*)
  uninstall                  remove arquivos gerenciados, preserva projetos (Administrador*)
  link NAME PATH             registra um projeto (PHP, Vite, Next, estático)
  unlink NAME                remove registro e rota
  links [FILTRO] [--json]    lista projetos registrados e descobertos
  park PATH                  registra uma pasta de projetos
  park ignore NAME|PATH      oculta um projeto estacionado da lista
  park unignore PATH         mostra novamente um projeto oculto
  unpark PATH                remove uma pasta estacionada
  parked                     lista pastas estacionadas

Servidores Dev e Estáticos:
  start NAME                 inicia o servidor de desenvolvimento do projeto
  stop NAME                  para o servidor de desenvolvimento do projeto
  restart NAME               reinicia o servidor de desenvolvimento
  build NAME                 executa build do projeto
  deps install NAME          instala dependências do projeto
  static NAME [DIR]          configura pasta de arquivos estáticos
  dev NAME [OPÇÕES]          configura ou gerencia servidor dev

Operação:
  gui                        inicia a interface gráfica desktop (Wails)
  status                     mostra componentes, projetos e URLs
  reload                     valida/aplica configurações e recarrega Caddy
  trust                      instala e confia na CA interna do Caddy (Administrador*)
  secure NAME|PATH           ativa HTTPS para um projeto (Administrador*)
  unsecure NAME|PATH         desativa HTTPS para um projeto
  doctor [NAME]              diagnostica host, runtime e projeto
  logs [COMPONENT]           mostra logs gerenciados
  open [NAME]                abre URL ou mostra o dashboard textual
  mode default MODE          define o modo global (php, dev, static, auto)
  mode NAME MODE|inherit     sobrescreve ou restaura herança
  config export [PATH]       exporta configuração sem credenciais
  config import PATH          valida e importa uma configuração
  diagnostic [PATH]           gera pacote único de diagnóstico sanitizado
  api serve|status            API local autenticada para CLI/UI/serviço
  service install|...         instala e controla o serviço Windows opcional
  startup enable|disable      configura início automático no login
  telemetry status|...        telemetria opt-in, sanitizada e manual
  update check|download       consulta/prepara artefato com SHA-256

Rotas e Segurança:
  route [NAME] [--port auto|N]
                             inspeciona ou sobrescreve a porta LAN
  expose NAME [--duration D]
                             expõe projeto temporariamente
  unexpose NAME              revoga exposição de projeto
  allowlist [default|NAME] [set|add|remove|clear CIDR...]
                             configura restrição de IPs/CIDRs
  auth enable|disable [default|NAME] [USER PASS]
                             configura autenticação HTTP básica
  ca info|export|rotate      gerencia CA interna e certificados
  security posture|audit     auditoria e postura de segurança

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
		"install":    "uso: devlan install [--no-firewall] [--windows-port PORT]",
		"uninstall":  "uso: devlan uninstall",
		"link":       "uso: devlan link NAME PATH",
		"unlink":     "uso: devlan unlink NAME",
		"links":      "uso: devlan links [FILTRO] [--json]",
		"park":       "uso: devlan park PATH | devlan park ignore NAME|PATH | devlan park unignore PATH",
		"unpark":     "uso: devlan unpark PATH",
		"parked":     "uso: devlan parked",
		"status":     "uso: devlan status",
		"reload":     "uso: devlan reload",
		"trust":      "uso: devlan trust",
		"secure":     "uso: devlan secure NAME|PATH",
		"unsecure":   "uso: devlan unsecure NAME|PATH",
		"doctor":     "uso: devlan doctor [NAME]",
		"logs":       "uso: devlan logs [COMPONENT]",
		"open":       "uso: devlan open [NAME]",
		"mode":       "uso: devlan mode default MODE | devlan mode NAME MODE|inherit",
		"route":      "uso: devlan route [NAME] [--port auto|PORT]",
		"expose":     "uso: devlan expose NAME [--duration 30m|1h|2h]",
		"unexpose":   "uso: devlan unexpose NAME",
		"allowlist":  "uso: devlan allowlist [default|NAME] [set|add|remove|clear CIDR...]",
		"auth":       "uso: devlan auth enable default|NAME USERNAME PASSWORD | devlan auth disable default|NAME",
		"ca":         "uso: devlan ca info | devlan ca export [PATH] | devlan ca rotate",
		"security":   "uso: devlan security posture | devlan security audit [--lines N]",
		"config":     "uso: devlan config export [PATH] | devlan config import PATH",
		"diagnostic": "uso: devlan diagnostic [PATH]",
		"api":        "uso: devlan api serve | devlan api status",
		"service":    "uso: devlan service install|remove|start|stop|status|run",
		"startup":    "uso: devlan startup enable [gui|service] | disable | status",
		"telemetry":  "uso: devlan telemetry status|enable ENDPOINT|disable|send",
		"update":     "uso: devlan update check CHANNEL [MANIFEST_URL] | devlan update download CHANNEL MANIFEST_URL PATH",
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

func runRoute(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 {
		cfg, err := service.Store.Load()
		if err != nil {
			return err
		}
		eff, err := service.EffectiveConfig(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Printf("Portas LAN: automáticas a partir de %d\n\n", cfg.RouteBasePort)
		writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "PROJETO\tPORTA LAN\tURL LOCAL\tURL LAN\tOVERRIDE")
		for _, p := range eff.Projects {
			port := eff.EffectiveRoutePort(p)
			override := "auto"
			if p.RoutePort != nil {
				override = "customizada"
			}
			fmt.Fprintf(writer, "%s\t%d\t%s\t%s\t%s\n", p.Name, port, domain.LocalDevURL(p.Name), routeURL(cfg, p, port), override)
		}
		return writer.Flush()
	}
	name := args[0]
	if len(args) == 1 {
		cfg, err := service.Store.Load()
		if err != nil {
			return err
		}
		eff, err := service.EffectiveConfig(ctx, cfg)
		if err != nil {
			return err
		}
		p, found := eff.Project(name)
		if !found {
			return fmt.Errorf("projeto não encontrado: %s", name)
		}
		override := "automática"
		if p.RoutePort != nil {
			override = "customizada"
		}
		fmt.Printf("Projeto %s: porta LAN %d (%s)\n", name, eff.EffectiveRoutePort(p), override)
		fmt.Printf("Local: %s\nLAN: %s\n", domain.LocalDevURL(p.Name), routeURL(cfg, p, eff.EffectiveRoutePort(p)))
		return nil
	}
	if len(args) != 3 || args[1] != "--port" {
		return fmt.Errorf("uso: devlan route NAME --port auto|PORT")
	}
	var port *int
	if args[2] != "auto" {
		parsed, err := strconv.Atoi(args[2])
		if err != nil || parsed < 1024 || parsed > 65535 {
			return fmt.Errorf("porta inválida: %s", args[2])
		}
		port = &parsed
	}
	res, err := service.SetRoutePort(ctx, name, port)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}
	fmt.Printf("Porta LAN do projeto %s atualizada.\n", name)
	return nil
}

func routeURL(cfg domain.Config, project domain.Project, port int) string {
	host := cfg.LANAddress
	if host == "" || host == "auto" {
		host, _ = platform.LANAddress()
		if host == "" {
			host = "localhost"
		}
	}
	resolved := domain.ResolvedProject{Project: project, RoutePort: port}
	return resolved.URL(host, cfg.WindowsPort, cfg.HTTPSPort, cfg.SecureProject(project))
}

func runExpose(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: devlan expose NAME [--duration 30m|1h|2h]")
	}
	name := args[0]
	var duration time.Duration
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--duration" && i+1 < len(args) {
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return fmt.Errorf("duração inválida: %s", args[i])
			}
			duration = d
		} else {
			return fmt.Errorf("opção desconhecida %s", arg)
		}
	}
	res, projName, err := service.ExposeProject(ctx, name, duration)
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}
	if duration > 0 {
		fmt.Printf("Projeto %s exposto temporariamente por %v.\n", projName, duration)
	} else {
		fmt.Printf("Projeto %s exposto.\n", projName)
	}
	return nil
}

func runUnexpose(ctx context.Context, service *app.App, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: devlan unexpose NAME")
	}
	res, projName, err := service.UnexposeProject(ctx, args[0])
	printWarnings(res.Warnings)
	if err != nil {
		return err
	}
	fmt.Printf("Exposição do projeto %s revogada.\n", projName)
	return nil
}

func runAllowlist(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 {
		cfg, err := service.Store.Load()
		if err != nil {
			return err
		}
		eff, err := service.EffectiveConfig(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Printf("Allowlist Global: %v\n\n", cfg.Allowlist)
		writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "PROJETO\tALLOWLIST\tORIGEM")
		for _, p := range eff.Projects {
			origin := "herdada (global)"
			if len(p.Allowlist) > 0 {
				origin = "específica do projeto"
			}
			al := eff.EffectiveAllowlist(p)
			fmt.Fprintf(writer, "%s\t%s\t%s\n", p.Name, strings.Join(al, ", "), origin)
		}
		return writer.Flush()
	}
	sub := args[0]
	switch sub {
	case "set":
		if len(args) < 3 {
			return fmt.Errorf("uso: devlan allowlist set default|NAME CIDR...")
		}
		res, err := service.SetAllowlist(ctx, args[1], args[2:])
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Allowlist de %s atualizada.\n", args[1])
		return nil
	case "add":
		if len(args) < 3 {
			return fmt.Errorf("uso: devlan allowlist add default|NAME CIDR...")
		}
		res, err := service.AddAllowlist(ctx, args[1], args[2:])
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("IPs/CIDRs adicionados à allowlist de %s.\n", args[1])
		return nil
	case "remove":
		if len(args) < 3 {
			return fmt.Errorf("uso: devlan allowlist remove default|NAME CIDR...")
		}
		res, err := service.RemoveAllowlist(ctx, args[1], args[2:])
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("IPs/CIDRs removidos da allowlist de %s.\n", args[1])
		return nil
	case "clear":
		if len(args) != 2 {
			return fmt.Errorf("uso: devlan allowlist clear default|NAME")
		}
		res, err := service.ClearAllowlist(ctx, args[1])
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Allowlist de %s limpa.\n", args[1])
		return nil
	default:
		cfg, err := service.Store.Load()
		if err != nil {
			return err
		}
		eff, err := service.EffectiveConfig(ctx, cfg)
		if err != nil {
			return err
		}
		p, found := eff.Project(args[0])
		if !found {
			return fmt.Errorf("projeto não encontrado: %s", args[0])
		}
		fmt.Printf("Allowlist efetiva de %s: %s\n", p.Name, strings.Join(eff.EffectiveAllowlist(p), ", "))
		return nil
	}
}

func runAuth(ctx context.Context, service *app.App, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("uso: devlan auth enable default|NAME USERNAME PASSWORD | devlan auth disable default|NAME")
	}
	action := args[0]
	target := args[1]
	switch action {
	case "enable":
		if len(args) != 4 {
			return fmt.Errorf("uso: devlan auth enable default|NAME USERNAME PASSWORD")
		}
		res, err := service.SetAuth(ctx, target, true, args[2], args[3])
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Autenticação HTTP ativada para %s (usuário: %s).\n", target, args[2])
		return nil
	case "disable":
		res, err := service.DisableAuth(ctx, target)
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Printf("Autenticação HTTP desativada para %s.\n", target)
		return nil
	default:
		return fmt.Errorf("ação desconhecida: %s (use enable ou disable)", action)
	}
}

func runCA(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 || args[0] == "info" {
		info, err := service.CAInfo(ctx)
		if err != nil {
			return err
		}
		fmt.Println("=== Autoridade Certificadora (CA) DevLAN ===")
		fmt.Printf("Caminho do certificado raiz: %s\n", info["path"])
		fmt.Printf("Certificado existente: %s\n", info["exists"])
		if s, ok := info["size"]; ok {
			fmt.Printf("Tamanho: %s\n", s)
		}
		fmt.Println("\nPara instalar no Android/iOS/outro computador:")
		fmt.Println("1. Exporte o arquivo: devlan ca export")
		fmt.Println("2. Ou baixe direto pelo navegador na LAN: http://<LAN_IP>/__devlan/ca.crt")
		return nil
	}
	switch args[0] {
	case "export":
		target := ""
		if len(args) > 1 {
			target = args[1]
		}
		savedPath, err := service.ExportCA(ctx, target)
		if err != nil {
			return err
		}
		fmt.Printf("Certificado raiz exportado com sucesso para: %s\n", savedPath)
		return nil
	case "rotate":
		res, err := service.RotateCA(ctx)
		printWarnings(res.Warnings)
		if err != nil {
			return err
		}
		fmt.Println("Rotação e recarregamento de certificados solicitados com sucesso.")
		return nil
	default:
		return fmt.Errorf("subcomando de ca desconhecido: %s (use info, export, rotate)", args[0])
	}
}

func runSecurity(ctx context.Context, service *app.App, args []string) error {
	if len(args) == 0 || args[0] == "posture" {
		cfg, err := service.Store.Load()
		if err != nil {
			return err
		}
		isPublic, netDetail, _ := platform.NetworkProfile(ctx)
		fmt.Println("=== Postura de Segurança DevLAN ===")
		if isPublic {
			fmt.Printf("[ALERTA] Perfil de rede: %s (recomenda-se modo Privado ou uso de Allowlist/Auth)\n", netDetail)
		} else {
			fmt.Println("[OK] Perfil de rede: Privada / confiável")
		}
		if len(cfg.Allowlist) > 0 {
			fmt.Printf("[OK] Allowlist global: %s\n", strings.Join(cfg.Allowlist, ", "))
		} else {
			fmt.Println("[INFO] Allowlist global: aberta na sub-rede")
		}
		if len(cfg.AuthUsers) > 0 {
			fmt.Printf("[OK] Basic Auth global: ativada (%d usuários)\n", len(cfg.AuthUsers))
		} else {
			fmt.Println("[INFO] Basic Auth global: desativada")
		}
		return nil
	}
	if args[0] == "audit" {
		lines := 50
		if len(args) >= 3 && args[1] == "--lines" {
			if l, err := strconv.Atoi(args[2]); err == nil && l > 0 {
				lines = l
			}
		}
		logs, err := service.SecurityAuditLogs(ctx, lines)
		if err != nil {
			return err
		}
		fmt.Print(logs)
		return nil
	}
	return fmt.Errorf("subcomando security desconhecido: %s (use posture, audit)", args[0])
}
