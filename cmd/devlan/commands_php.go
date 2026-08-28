package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/dougkusanagi/dev-lan/internal/app"
	"github.com/dougkusanagi/dev-lan/internal/domain"
	"github.com/dougkusanagi/dev-lan/internal/platform"
)

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
		versions, err := service.PHPVersions(platform.WithWSLOperation(ctx, platform.WSLOperationStatus))
		if err != nil {
			return err
		}
		cfg, err := service.Config()
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
			cfg, err := service.Config()
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
	cfg, err := service.Config()
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
