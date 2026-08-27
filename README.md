# DevLAN

DevLAN é uma ferramenta para publicar projetos que rodam no WSL para outros dispositivos da rede local. O control plane fica no Windows e a borda é um único Caddy no WSL com rede espelhada.

Cada projeto possui duas origens fixas:

```text
https://meu-projeto.localhost/
http://192.168.1.50:8080/
```

O núcleo PHP suporta Laravel, Symfony e aplicações genéricas, inclusive com
múltiplas versões de PHP em paralelo. Sites estáticos, servidores JavaScript
sob demanda, dashboard e menu na área de notificação já estão disponíveis; a
operação persistente e a recuperação evoluem na Fase 6.

## Princípios

- A CLI e a interface gráfica usam o mesmo núcleo em Go.
- O Windows controla estado, API local e coordena Windows Firewall/Hyper-V Firewall.
- O WSL executa o Caddy único, PHP-FPM e os processos dos projetos.
- Configurações geradas são validadas antes de serem aplicadas.
- Cada projeto pode sobrescrever padrões globais.
- A primeira versão deve resolver bem Laravel/PHP antes de crescer.
- Projetos nunca são publicados sem terem sido registrados explicitamente por `link` ou `park`.

## Arquitetura resumida

```text
Dispositivo na LAN
       │ mirrored networking
       ▼
Caddy único no WSL :80 / :443 / portas LAN
       │
       ▼
PHP-FPM ── projeto Laravel
```

Projetos JavaScript são atendidos por um supervisor no WSL, que inicia o
script `dev` sob demanda e encerra o processo depois de um período ocioso.

## Experiência esperada no MVP

```powershell
devlan install
devlan park /home/silver/dev
devlan link financeiro /home/silver/dev/financeiro
devlan status
devlan doctor
devlan secure financeiro
devlan open financeiro
```

Resultado:

```text
local  https://financeiro.localhost/
LAN    http://192.168.1.50:8080/
```

O comando `devlan route financeiro --port PORT` configura um override da porta
LAN; `devlan route financeiro --port auto` restaura a alocação automática.
As alocações automáticas são persistidas pelo caminho do projeto e podem ser
inspecionadas ou limpas explicitamente com `devlan route allocations`.

## Documentos

- [Arquitetura](docs/ARCHITECTURE.md)
- [CLI e configuração](docs/CLI-AND-CONFIG.md)
- [Instalação](docs/INSTALL.md)
- [Roadmap](docs/ROADMAP.md)
- [Operação, recuperação e suporte](docs/OPERATIONS.md)
- [Plano de reimplementação da interface Wails](docs/UI-REIMPLEMENTATION-PLAN.md)
- [Decisões técnicas](docs/DECISIONS.md)

## Fora do escopo da primeira instalação

- Distribuição automática da CA para outros dispositivos.
- Containers e bancos de dados.
- Instalação automática de dependências dos projetos.

## Implementação atual

O núcleo está implementado como uma CLI Go em `cmd/devlan`. Ele cobre:

- registro explícito com `link` e descoberta de filhos PHP por `park`;
- resolução `modo do projeto > park > padrão global`;
- detecção sem executar scripts, com presets Laravel, Symfony e PHP genérico;
- múltiplas versões PHP, extensões por versão, Composer versionado e pools
  `ondemand` compartilhados ou isolados;
- Caddyfile WSL determinístico, com uma porta LAN dedicada por projeto
  e contexto FastCGI na raiz;
- HTTPS opcional por CA interna na porta dedicada do projeto, selecionado com
  `devlan secure NAME|PATH`, e retorno a HTTP com `devlan unsecure NAME|PATH`;
- aplicação por arquivo temporário, validação quando o Caddy está disponível e rollback da última configuração funcional;
- `install`, `uninstall`, `status`, `reload`, `doctor`, `logs`, `open` e os comandos de registro;
- alocação estável por caminho (`route_port_count`), com `route allocations`
  e prune dry-run;
- `php install|list|remove|use|extensions|pool|preset|info` e `composer`;
- regra de firewall DevLAN reconciliada por especificação, limitada ao perfil
  privado e à sub-rede local.
- serviço Windows opcional, inicialização no login e API local autenticada por token;
- exportação/importação sem credenciais, diagnóstico ZIP sanitizado e telemetria opt-in;
- preparação de updates `stable`/`preview` com validação SHA-256.

`devlan install` prepara os arquivos gerenciados e tenta criar as regras de
firewall. Para uma máquina limpa, use o [bootstrap de instalação](docs/INSTALL.md):
ele instala Go, WSL/Ubuntu, Caddy no WSL, PHP-FPM 8.5, extensões Laravel e
Composer, compila a CLI e executa `devlan install`. Depois de salvar o trabalho
nas distribuições, aplique `devlan topology migrate --yes`; o comando encerra
todas as distribuições WSL. Dependências dos projetos continuam explícitas; o
bootstrap não executa `composer install` automaticamente.

Os procedimentos da Fase 6 estão em [docs/OPERATIONS.md](docs/OPERATIONS.md).

## Instalação rápida via curl

Abra o **PowerShell como Administrador**. A elevação é necessária para instalar
ou configurar WSL e as regras de firewall. Em seguida, execute:

```powershell
curl.exe -fsSL https://raw.githubusercontent.com/dougkusanagi/dev-lan/master/scripts/install.ps1 -o "$env:TEMP\devlan-install.ps1"; powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$env:TEMP\devlan-install.ps1"
```

O script de bootstrap instala o executável e suas dependências; ao final ele
executa `devlan install`, que cria ou reconcilia a configuração gerenciada. Com
a CLI já instalada, `devlan install` é o comando padrão e idempotente para
configurar novamente o ambiente. O fluxo completo e as opções estão em
[docs/INSTALL.md](docs/INSTALL.md).

## Desenvolvimento

Com Go instalado:

```powershell
go test ./...
go vet ./...
go run ./cmd/devlan help
```

Durante o desenvolvimento, `--data-dir` permite usar um diretório isolado:

```powershell
go run ./cmd/devlan --data-dir .devlan install
go run ./cmd/devlan --data-dir .devlan doctor
```

Para compilar a CLI com suporte à interface desktop no Windows, use o script:

```powershell
.\scripts\build.ps1
devlan gui
```

Para escolher outro executável, use `.\scripts\build.ps1 -OutputPath .\bin\devlan.exe`.
Um `go build` sem essas tags compila a CLI, mas o Wails exibe uma mensagem
informando que a aplicação desktop foi construída sem as tags corretas.
