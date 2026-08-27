# Decisões técnicas — histórico

> Documento substituído por [Arquitetura](../ARCHITECTURE.md) e
> [ADRs](../adr/README.md). Preserve somente para contexto histórico.

## Go como núcleo

Go será usado para CLI, domínio, instalação, diagnósticos e processos. Motivos:

- executável único e distribuição simples no Windows;
- boa biblioteca padrão para HTTP, processos e concorrência;
- baixo tempo de inicialização;
- possibilidade de reutilizar o núcleo na interface Wails;
- cross-compilation para um pequeno agente Linux no WSL.

A CLI planejada para o WSL será um cliente fino do controlador Windows. Ela
resolverá caminhos Linux e a distribuição corrente, mas não possuirá banco ou
configuração independentes. Essa decisão evita divergência entre duas CLIs e
preserva uma única ordem para geração, validação, reload e rollback.

O domínio não deve importar Wails, Cobra, Caddy ou detalhes de PowerShell diretamente. Essas integrações ficam em adaptadores para permitir testes e evolução.

## Bootstrap e versão PHP do MVP

O bootstrap é PowerShell no Windows e usa um script Bash fixo no WSL. Go é
baixado da lista oficial de releases estáveis quando não está no PATH; Caddy
usa o pacote oficial/distribuição ou o release oficial com checksum. PHP não
tem uma modalidade LTS oficial: o MVP adota 8.5 por padrão, permite 8.3/8.4 e
detecta a versão efetivamente instalada. O instalador não executa `composer
install` nem qualquer script de projeto.

## Versões PHP e pools na Fase 2

Versões instaladas são registradas no estado do DevLAN, enquanto preferências
globais continuam no TOML. Cada branch recebe um mestre PHP-FPM independente;
isso evita que um projeto em PHP 8.3 dependa do socket ou do worker pool de PHP
8.5. O padrão é um pool compartilhado por versão, com `pm=ondemand`; um
override por projeto pode criar um pool isolado.

O socket legado `/run/php/php-fpm.sock` é preservado quando não há versões
registradas, para que upgrades da Fase 1 não quebrem a instalação existente.
Composer usa `phpVERSAO composer` no ambiente `per-version` e nunca executa
scripts de projeto durante a detecção, instalação ou reload.

## Wails para a interface

Wails ocupa para Go um papel semelhante ao Tauri para Rust: backend nativo e interface feita com tecnologias web, renderizada pela WebView do sistema.

A interface será adicionada depois que o núcleo da CLI estiver estável. Ela usará:

- Wails;
- frontend web pequeno em TypeScript;
- Tailwind compilado;
- poucos componentes e estado local simples;
- bindings tipados gerados entre Go e TypeScript.

O objetivo não é criar outra implementação das regras. CLI e UI chamam os mesmos serviços do domínio.

Wails v3 oferece uma API moderna e suporte integrado a system tray, mas ainda está em beta em agosto de 2026. A versão deve ser fixada no projeto, testada no Windows-alvo e encapsulada atrás de um adaptador de UI. Se o beta se mostrar instável durante a fase de interface, a decisão deve ser revisitada sem afetar o núcleo.

Referências:

- https://v3.wails.io/
- https://v3.wails.io/features/menus/systray/
- https://wails.io/docs/introduction/

## Interface proposta

Uma janela pequena com:

- lista de projetos e estado;
- URL com ação de copiar/abrir;
- PHP ou runtime detectado;
- iniciar, parar e reiniciar;
- últimas linhas de log;
- diagnóstico e correções orientadas;
- configurações globais e sobrescritas do projeto.

O menu na tray terá apenas ações frequentes. Configuração detalhada fica na janela.

## Serviço Windows

O MVP não precisa começar como serviço permanente. O instalador pode realizar operações elevadas e configurar o Caddy, enquanto a CLI opera no usuário comum.

Se for necessário iniciar antes do login ou manter supervisão contínua, será criado um serviço Windows separado da interface. Serviços não desenham UI na sessão do usuário; Wails/tray será o cliente visual desse serviço.

## Caddy único no WSL com rede espelhada

A decisão anterior de manter dois Caddys foi substituída pelo
[ADR 0006](../adr/0006-caddy-unico-wsl-mirrored.md). Em Windows 11 22H2+ com WSL 2
e `networkingMode=mirrored`, um único Caddy systemd no WSL é dono da borda
HTTP/HTTPS/LAN. O Windows continua dono do estado, control plane, API local e
coordenação de firewall; o dashboard é o único upstream para
`127.0.0.1:<ui_port>`.

## TLS por projeto na borda WSL, com CA interna

TLS é propriedade da borda WSL. `secure` continua sendo uma preferência do
projeto, enquanto a negociação e a CA permanecem no Caddy único. A origem
local é sempre HTTPS em `.localhost`; a origem LAN usa HTTP ou HTTPS na porta
dedicada conforme a configuração global e a preferência do projeto. Somente o
certificado raiz público pode ser exportado para o Windows ou para clientes;
chaves privadas permanecem no WSL.

Uma autoridade pública não é pressuposta para IPs privados. A CA interna
permite criptografia na LAN, porém cada dispositivo cliente precisa confiar no
certificado raiz. Automação de distribuição, rotação e revogação permanece uma
evolução posterior de segurança.

## Roteamento

Todo projeto expõe duas origens fixas simultaneamente:

- local: `https://nome.localhost/`;
- LAN: `http(s)://IP:porta/`.

Não existe modo selecionável, hostname customizado, subpath externo, edição de
`hosts` ou DNS. A porta LAN é automática a partir de `route_base_port`, com
override explícito por projeto e `--port auto` para restaurar o cálculo.

## Transporte de controle na Fase 6

O serviço Windows e os clientes locais usam uma API HTTP limitada a loopback,
com token aleatório persistido fora do estado exportável. O token foi escolhido
como primeiro transporte porque funciona entre processo Go, Wails e CLI sem
exigir uma dependência de mTLS ou uma segunda implementação de protocolo; o
binding em `127.0.0.1`, a validação do endpoint e a comparação em tempo
constante reduzem a superfície enquanto IPC nativo continua opcional.

Exportação e diagnóstico têm pipelines separados e sanitizados. Nenhum deles
transporta hashes de autenticação, exposições temporárias, variáveis de
ambiente ou conteúdo de projetos. Atualizações são baixadas para um estágio
temporário e verificadas por SHA-256; a substituição automática permanece
dependente do instalador Windows assinado e do pipeline de releases.
