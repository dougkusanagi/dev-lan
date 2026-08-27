# Plano de substituição do roteamento por subpath

> Documento histórico anterior ao Marco 8. A fronteira de execução foi
> consolidada em um único Caddy WSL com rede espelhada; consulte
> [ADR 0005](adr/0005-caddy-unico-wsl-mirrored.md),
> [ARCHITECTURE.md](ARCHITECTURE.md) e [ROADMAP.md](ROADMAP.md) para o contrato
> vigente. Este plano permanece apenas como registro das decisões que levaram
> à remoção de subpath/host.

## Status e documentos relacionados

Este documento registra o contrato vigente e as decisões que o antecederam.
O smoke real dependente de Windows/WSL continua opt-in. A tasklist canônica
está em [ROADMAP.md](ROADMAP.md) e os achados transversais em
[ENGINEERING-HARDENING-PLAN.md](ENGINEERING-HARDENING-PLAN.md).

## Decisão final

O DevLAN não terá modos de roteamento selecionáveis. Cada projeto será exposto
simultaneamente por duas origens com finalidades fixas:

```text
desenvolvimento no Windows: https://nome-do-projeto.localhost/
acesso pela LAN:            http://IP-DO-WINDOWS:PORTA/
```

Consequências:

- `path` deixa de existir completamente;
- `host` deixa de ser um modo configurável;
- `.localhost` é a origem local automática de todo projeto;
- porta dedicada é a única origem oferecida à LAN;
- `RouteMode`, `DefaultRouteMode`, `RouteHost`, `DomainSuffix` e herança de
  modo por park/projeto podem ser removidos;
- permanece somente a política de alocação/override da porta LAN;
- não haverá migração, compatibilidade ou rollback para `path`, pois o produto
  ainda não possui usuários nem estado publicado a preservar;
- DNS interno e Ubuntu Server ficam fora deste plano.

Essa simplificação evita apresentar como escolha dois endereços que podem e
devem coexistir. O Marco 8 mantém o control plane no Windows, mas consolida a
borda em um único Caddy WSL; os artefatos do desenho anterior só existem para
leitura/rollback de upgrades.

## Motivação

Aplicações completas frequentemente assumem estar na raiz `/`. No subpath
`/projeto/`, assets `/storage/*`, redirects `/login`, cookies, `fetch`, CSS,
WebSockets, HMR, service workers e callbacks de autenticação podem escapar do
projeto. O caso foi reproduzido com `cj-catalogo`: funciona em
`cj-catalogo.localhost`, mas perde assets absolutos pelo subpath.

Uma origem própria resolve o problema sem reescrever respostas. Localmente o
hostname separa origens; na LAN a porta separa o roteamento. O proxy não deve
depender de `Referer` nem reescrever HTML, CSS ou JavaScript.

## Topologia recomendada

```text
┌──────────────────────────── Windows ────────────────────────────┐
│ UI/CLI/serviço DevLAN (control plane e estado autoritativo)     │
│   ├─ servidor web: SPA + API em porta administrativa            │
│   ├─ Caddy WSL único: *.localhost + portas LAN + TLS             │
│   ├─ Firewall/CA/startup/update                                 │
│   └─ adapter WSL via wsl.exe                                   │
└──────────────────────────────┬──────────────────────────────────┘
                               │ comandos estruturados
┌──────────────────────────── WSL/Ubuntu ─────────────────────────┐
│ Caddy WSL + PHP-FPM + projetos + processos JS                  │
│ opcional futuro: agente Linux estreito e persistente           │
└─────────────────────────────────────────────────────────────────┘
```

O núcleo continua no Windows. O WSL executa as responsabilidades que dependem
do filesystem/runtimes Linux. Essa fronteira já corresponde ao produto: o
estado, UI, serviço, firewall e CA são Windows; o Caddy de borda, PHP e
projetos são Linux.

### Por que não instalar o núcleo inteiro no WSL

Mover o control plane para o WSL aproxima o núcleo dos projetos, mas desloca
para o lado errado as responsabilidades mais privilegiadas e específicas:

- firewall, perfil de rede, serviço/startup e CA pertencem ao Windows; o Caddy
  de borda pertence ao WSL;
- Wails/tray continuam sendo processos Windows e precisariam de IPC com WSL;
- estado, token, lifecycle e updates passariam a atravessar dois sistemas;
- um daemon no WSL depende de inicialização/disponibilidade da distribuição;
- comandos Windows iniciados do WSL continuam enfrentando UAC e elevação não
  interativa; executar `powershell.exe` do Linux não elimina essa fronteira;
- seriam necessários protocolo, autenticação, versionamento, reconexão e
  recuperação para uma instalação que hoje pode ser um único control plane.

Em sentido contrário, `wsl.exe --distribution ... -- comando args...` é uma
fronteira suportada e suficiente para operações de baixa frequência. Seu custo
de spawn e a quantidade de shell scripts são dívidas de performance e
testabilidade, não um motivo arquitetural suficiente para mover tudo.

### Evolução preferida se `wsl.exe` virar problema

Não duplicar o domínio nem mover a autoridade. Introduzir um agente Linux
pequeno no WSL somente após medir necessidade:

- Windows continua dono do desired state e da transação;
- agente WSL possui API estreita/versionada para discovery, validate/reload do
  Caddy WSL, PHP e processos JS;
- uma chamada em lote substitui vários spawns de `wsl.exe`;
- transporte começa por stdio JSON sobre um único `wsl.exe` persistente ou
  socket protegido; TCP só entra com autenticação e necessidade clara;
- operações são idempotentes, canceláveis e retornam erros estruturados;
- incompatibilidade de versão falha antes de aplicar mudanças;
- sem agente disponível, `doctor` informa degraded; não mantém dois estados.

Esse desenho obtém proximidade com o Linux sem obrigar o Linux a administrar o
host Windows. Não existe requisito de instalar este DevLAN em Ubuntu Server.
Portabilidade Linux futura exigiria outro produto/adapter de borda e uma nova
decisão explícita.

## Interface administrativa web

A interface gráfica será uma aplicação web servida pelo control plane Windows.
Ela terá duas URLs locais para a mesma SPA/API:

```text
http://127.0.0.1:3210/
https://devlan.localhost/
```

`3210` é o default planejado e deve ser configurável. A porta administrativa é
reservada antes do pool de projetos e das portas de runtime. A URL por host
passa pelo Caddy WSL único até `127.0.0.1:ui_port`; a URL por porta chega
diretamente ao servidor web Go. Ambas atendem o mesmo build e o mesmo contrato
HTTP, sem backends de UI duplicados.

### Papel do Wails

O browser será a superfície canônica. Durante a transição, Wails/tray pode
continuar como shell opcional para abrir a UI, notificações e integração com o
desktop, mas não manterá uma segunda API de domínio. O frontend React usa uma
interface `DevLANClient` única:

- adapter HTTP no navegador;
- adapter HTTP ou compatível no shell Wails;
- fake explícito para testes/component catalog.

Depois da paridade, manter ou remover a janela Wails será uma decisão pequena:
o núcleo e a UI não dependerão dela.

### Segurança local

Por padrão, o servidor administrativo faz bind somente em `127.0.0.1` e `::1`.
`devlan.localhost` é aceito somente de loopback. A API administrativa existente
não deve entregar ao JavaScript o token persistido em disco.

O contrato web deve incluir:

- allowlist estrita de `Host` e `Origin` para impedir DNS rebinding;
- sessão local e token anti-CSRF para toda mutação;
- cookies `HttpOnly` e `SameSite=Strict` onde forem usados;
- CSP, `frame-ancestors 'none'`, `nosniff` e política de referrer;
- nenhum segredo em HTML, URL, local storage, logs ou DTOs;
- métodos HTTP corretos; GET nunca altera estado;
- limite de body/header, timeouts e cancelamento;
- WebSocket/SSE autenticado se progresso/logs em tempo real forem adicionados;
- auditoria das operações privilegiadas e destrutivas;
- shutdown gracioso e mensagem clara quando o backend estiver indisponível.

A navegação direta pela porta deve ser funcional, não apenas redirect. Como as
origens `127.0.0.1:3210` e `devlan.localhost` não compartilham cookies/storage,
cada uma cria sua própria sessão local; o estado autoritativo continua no
backend.

### Acesso administrativo pela LAN

Não é habilitado implicitamente. Uma futura opção explícita
`ui_access = "lan"` pode fazer bind no endereço LAN e abrir somente a porta
administrativa na regra gerenciada. Nesse caso são requisitos de bloqueio:

- autenticação própria para a UI, sem reutilizar o token de arquivo;
- TLS e certificado confiável pelo cliente;
- `Private` + `LocalSubnet` e allowlist opcional;
- proteção contra brute force/rate limiting e sessões revogáveis;
- confirmação destacada antes de habilitar e `doctor` específico;
- nenhuma confiança baseada apenas no IP de origem.

Até esses requisitos estarem implementados/testados, “acesso por porta”
significa `127.0.0.1:3210`, somente na máquina Windows.

### Contrato de entrega

- o executável incorpora ou serve assets versionados do build frontend;
- `/api/v1/*` é same-origin nas duas URLs e versionado independentemente da UI;
- history fallback da SPA nunca intercepta `/api/*`, health ou assets ausentes;
- assets usam hash/cache imutável; o HTML de entrada usa no-cache;
- versão do frontend incompatível com a API produz tela de atualização, não
  comportamento parcial;
- `devlan gui` e o ícone da tray abrem `https://devlan.localhost/`, com fallback
  acionável para a porta se Caddy/CA não estiver disponível;
- o nome de projeto `devlan` é reservado para não colidir com o host da UI.

## Origem local `.localhost`

`.localhost` resolve para loopback pelo sistema/navegador. O DevLAN não deve
editar o arquivo `hosts`, criar registros DNS nem possuir `domain_suffix` para
esse fluxo.

Gerenciar a origem local significa:

- normalizar o nome do projeto e impedir duplicidade;
- gerar `https://nome.localhost` no Caddy WSL único;
- aceitar esse virtual host somente de loopback;
- emitir certificado pela CA local e diagnosticar sua confiança;
- encaminhar ao projeto correto no WSL, sempre na raiz `/`;
- remover artefatos gerenciados ao desvincular o projeto.

Requisitos:

- URL local não depende da disponibilidade/IP da LAN;
- cookies, storage e service workers ficam isolados por hostname;
- HMR/WebSocket usam a mesma origem;
- HTTP redireciona para HTTPS local quando a política assim definir;
- nomes inválidos/reservados falham antes de renderizar Caddyfile;
- acesso de outro dispositivo a `*.localhost` não é anunciado nem suportado.

## Origem LAN por porta

Cada projeto recebe uma porta persistida na borda Windows:

```text
http://192.168.10.77:8080/ -> projeto A
http://192.168.10.77:8081/ -> projeto B
```

O Caddy WSL único remove headers internos enviados pelo cliente, reconstrói os
forwarded headers confiáveis e serve o projeto diretamente na raiz `/`.

Requisitos:

- caminhos absolutos e redirects chegam ao projeto correto;
- scheme, host e porta externos são preservados nos forwarded headers;
- nenhuma decisão depende de `Referer`;
- WebSocket/HMR e HTTPS funcionam em PHP, static, Vite e SSR;
- dois projetos podem expor o mesmo caminho sem colisão;
- existe listener somente para porta atribuída;
- firewall cobre o pool apenas em `Private` + `LocalSubnet`.

Portas não isolam cookies HTTP no mesmo IP/hostname. Como `.localhost` é a
origem de desenvolvimento principal, a UI deve explicar essa limitação ao
copiar a URL LAN. O proxy não renomeará cookies.

## Modelo simplificado

Configuração global planejada:

```toml
route_base_port = 8080
route_port_count = 100
```

Estado planejado:

```json
{
  "route_port_allocations": {
    "/home/silver/Sites/cj-catalogo": 8081
  }
}
```

Projeto pode ter apenas override opcional:

```json
{
  "route_port": 8090
}
```

Não permanecem `route_mode`, `default_route_mode`, `route_host` ou
`domain_suffix`. Parks não herdam modo; projetos descobertos por park recebem
porta persistida sem precisar ser materializados apenas por isso.

A configuração administrativa é independente:

```toml
ui_port = 3210
ui_access = "local" # local; lan somente após o hardening correspondente
```

## Remoção direta de `path` e `host` configurável

Como não há usuários, a implementação deve remover numa única mudança:

- enums, parsing e resolução de `RouteMode`;
- campos globais, de park e de projeto relacionados a modo/hostname;
- renderers/matchers `path`, `handle_path` e fallback por `Referer`;
- renderers de hostname LAN arbitrário;
- comandos `route default`, `route migrate` e troca de modo;
- opções correspondentes da API, Wails e UI;
- `dns entries`, `dns sync` e documentação de DNS se não tiverem outra função;
- testes/fixtures que afirmem compatibilidade com comportamento removido.

Arquivos antigos de desenvolvimento podem ser descartados/recriados. Não criar
schema de compatibilidade para estado que nunca foi distribuído. Ainda assim,
o commit deve ser atômico e manter os testes verdes durante a refatoração.

## Alocação estável de portas

O cálculo atual `route_base_port + índice` troca URLs quando a ordem muda. A
alocação deve obedecer:

1. override explícito válido prevalece;
2. alocação existente por caminho normalizado é reutilizada;
3. primeiro valor livre do pool é escolhido deterministicamente;
4. HTTP, HTTPS, WSL, runtimes, overrides e alocações são reservados;
5. listeners externos são verificados antes de aplicar;
6. lote reserva tudo antes de persistir qualquer item;
7. exaustão falha sem mudança parcial;
8. projeto temporariamente ausente não perde a porta;
9. limpeza de órfãos é explícita e tem dry-run.

```powershell
devlan route allocations
devlan route allocations prune --dry-run
devlan route allocations prune
devlan route PROJECT --port 8090
devlan route PROJECT --port auto
```

## Firewall e listeners

Uma função pura calcula a especificação desejada. Install, route, TLS, repair,
doctor e UI usam a mesma fonte. A porta administrativa local não entra no
firewall; ela só entra quando `ui_access = "lan"` for explicitamente suportado.

```go
type PortRange struct { From, To int }
type FirewallSpec struct {
    Ports  []int
    Ranges []PortRange
}
```

Política padrão:

```text
TCP 80,443,8080-8179
perfil Private
origem LocalSubnet
```

O firewall pode cobrir o pool; Caddy escuta somente portas alocadas. Porta fora
do pool exige reconciliação elevada ou estado `degraded` com reparo exato. A
regra deve ser identificada inequivocamente e nunca sobrescrever regra alheia.
`doctor` compara direção, ação, protocolo, portas, perfil e origem.

## Aplicação consistente

A refatoração depende do coordenador transacional descrito no plano de
endurecimento. A unidade inclui config/estado, Caddyfiles, PHP, firewall e
reload dos processos afetados.

Fases: lock/revisão, plan, validate, stage, commit, reload/healthcheck e
finalize. Falha pós-reload restaura os artefatos e recarrega o snapshot
anterior. Isso é rollback operacional geral, não compatibilidade com `path`.

## CLI, UI e diagnóstico

Cada projeto exibe as duas URLs juntas:

```text
cj-catalogo
  local  https://cj-catalogo.localhost/       tls: ok
  lan    http://192.168.10.77:8081/           listener: ok  firewall: ok
```

Configuração de rota contém somente `Porta LAN: automática|customizada`.
Não há seletor de modo, hostname customizado, suffix, DNS ou subpath. Toda
mutação passa por `internal/app.App`; frontend não chama `netsh` ou Caddy.

A própria UI abre por `https://devlan.localhost/` ou pela porta administrativa.
Seu estado de saúde mostra separadamente servidor web, API, Caddy WSL único,
systemd, mirrored networking, CA e firewall; a UI não pode declarar o sistema
saudável apenas porque seus assets carregaram.

## Estratégia de testes

### Unitários/property/fuzz

- estabilidade, unicidade, conflito, exaustão, parks e órfãos do alocador;
- validação/duplicidade de nomes `.localhost`;
- reserva/conflito da porta administrativa e do nome `devlan`;
- cálculo e reconciliação idempotente de firewall;
- headers internos removidos e forwarded headers corretos;
- busca automatizada impede reintrodução dos campos/modos removidos;
- renderização determinística com relógio injetado.

### Integração

Duas fixtures expõem os mesmos assets, redirect, endpoint de origem e
WebSocket. Validar simultaneamente `.localhost` e portas distintas com um
Caddy WSL real: raiz `/`, assets sem `Referer`, redirects, HTTPS, HMR, cookies,
spoof de headers, falhas transacionais e healthcheck.

Para a interface, testar as duas origens, history fallback, version mismatch,
Host/Origin inválidos, CSRF, CSP, sessão independente, API indisponível e
ausência de segredos no bundle do navegador.

### Ambiente real

- Windows em perfil privado com WSL2;
- navegador Windows em `*.localhost`;
- outro dispositivo na LAN por `IP:porta`;
- PHP/Laravel, static, Vite e SSR;
- CA confiável/não confiável, conflito de listener e falta de elevação.

## Critérios de aceite

- `RouteModePath`, `RouteModeHost` e seus campos/comandos não existem;
- todo projeto possui URL local `.localhost` e porta LAN estável;
- a mesma GUI funciona em `127.0.0.1:3210` e `devlan.localhost` com API
  same-origin e sem token persistido exposto ao browser;
- a UI administrativa não fica acessível pela LAN por padrão;
- `cj-catalogo` carrega assets absolutos localmente e pela LAN;
- `.localhost` funciona sem `hosts` ou DNS e só em loopback;
- nenhuma rota depende de `Referer`;
- portas sobrevivem a reload, reboot, reordenação e parks;
- firewall, listeners e URLs concordam;
- rollback geral restaura arquivos e processos;
- testes unitários, integração, race, frontend e smoke real passam.

## Riscos

- **Muitas portas:** pool pequeno, firewall restrito e listeners sob demanda.
- **Cookies na LAN:** limitação explícita; desenvolvimento principal usa hosts
  locais isolados.
- **Porta ocupada:** diagnóstico/override, nunca realocação silenciosa.
- **Elevação ausente:** estado degraded acionável.
- **Muitos spawns `wsl.exe`:** medir, agrupar chamadas e só então introduzir
  agente persistente; não mover prematuramente o control plane.
