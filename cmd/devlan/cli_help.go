package main

import (
	"fmt"
)

func printUsage() {
	fmt.Print(`DevLAN — publicar projetos PHP, JavaScript e estáticos do WSL na rede local

Uso:
  devlan [--data-dir DIR] COMANDO [ARGUMENTOS]

Fundação e registro:
  install [--no-firewall] [--windows-port PORT]
                              inicializa arquivos gerenciados (Administrador*)
  uninstall [OPÇÕES]         remove o DevLAN, restaura configurações e preserva projetos (Administrador*)
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
  gui [--foreground]         inicia o dashboard web no navegador (devlan.localhost)
  desktop install|...        instala/gerencia atalhos e integração de desktop
  status                     mostra componentes, projetos e URLs
  topology status|check      mostra topologia Caddy e compatibilidade WSL
  topology repair            reconcilia Caddy, firewall e .wslconfig sem shutdown
  topology migrate --yes     migra com backup para o Caddy único no WSL
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
  service install --system|... instala o serviço Windows opcional (avançado)
  startup enable|disable      configura início automático no login
  telemetry status|...        telemetria opt-in, sanitizada e manual
  update check|download       consulta/prepara artefato com SHA-256

Rotas e Segurança:
  route [NAME] [--port auto|N]
                             inspeciona ou sobrescreve a porta LAN
  route allocations          lista alocações persistidas
  route allocations prune [--dry-run]
                             remove órfãos de forma explícita
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
		"uninstall":  "uso: devlan uninstall [--dry-run] [--keep-data] [--keep-dependencies] [--purge --yes] [--json]",
		"link":       "uso: devlan link NAME PATH",
		"unlink":     "uso: devlan unlink NAME",
		"links":      "uso: devlan links [FILTRO] [--json]",
		"park":       "uso: devlan park PATH | devlan park ignore NAME|PATH | devlan park unignore PATH",
		"unpark":     "uso: devlan unpark PATH",
		"parked":     "uso: devlan parked",
		"gui":        "uso: devlan gui [--foreground]",
		"desktop":    "uso: devlan desktop install | status | uninstall",
		"status":     "uso: devlan status",
		"topology":   "uso: devlan topology status|check|repair|migrate [--yes]",
		"reload":     "uso: devlan reload",
		"trust":      "uso: devlan trust",
		"secure":     "uso: devlan secure NAME|PATH",
		"unsecure":   "uso: devlan unsecure NAME|PATH",
		"doctor":     "uso: devlan doctor [NAME]",
		"logs":       "uso: devlan logs [COMPONENT]",
		"open":       "uso: devlan open [NAME]",
		"mode":       "uso: devlan mode default MODE | devlan mode NAME MODE|inherit",
		"route":      "uso: devlan route [NAME] [--port auto|PORT] | devlan route allocations [prune [--dry-run]]",
		"expose":     "uso: devlan expose NAME [--duration 30m|1h|2h]",
		"unexpose":   "uso: devlan unexpose NAME",
		"allowlist":  "uso: devlan allowlist [default|NAME] [set|add|remove|clear CIDR...]",
		"auth":       "uso: devlan auth enable default|NAME USERNAME PASSWORD | devlan auth disable default|NAME",
		"ca":         "uso: devlan ca info | devlan ca export [PATH] | devlan ca rotate",
		"security":   "uso: devlan security posture | devlan security audit [--lines N]",
		"config":     "uso: devlan config export [PATH] | devlan config import PATH",
		"diagnostic": "uso: devlan diagnostic [PATH]",
		"api":        "uso: devlan api serve | devlan api status",
		"service":    "uso: devlan service install --system | remove|start|stop|status|run",
		"startup":    "uso: devlan startup enable [gui|service] | disable | status",
		"telemetry":  "uso: devlan telemetry status|enable ENDPOINT|disable|send",
		"update":     "uso: devlan update check CHANNEL [MANIFEST_URL] | devlan update download CHANNEL MANIFEST_URL PATH",
	}
	if usage, ok := usages[command]; ok {
		fmt.Printf("%s\n\nOpções:\n  -h, --help    mostra esta ajuda\n", usage)
		switch command {
		case "install", "uninstall":
			fmt.Println("\nAdministrador: necessário para criar/remover a regra de firewall e limpar integrações do sistema.")
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
