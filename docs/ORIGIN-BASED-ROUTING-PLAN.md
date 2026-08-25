# Plano de roteamento por origem para aplicações na LAN

## Contexto

O DevLAN publica projetos na rede local usando três modos de rota:

- `path`: `http://IP/projeto/`;
- `port`: `http://IP:PORTA/`;
- `host`: `http://projeto.dominio/`.

O modo `path` é atualmente o padrão. Ele funciona quando a aplicação conhece o
prefixo sob o qual foi publicada, mas não é transparente para aplicações web
que assumem estar na raiz `/`.

O problema foi reproduzido com `cj-catalogo`:

- `GET /cj-catalogo/` retorna `200`;
- o HTML gera recursos como `/storage/...` e `/img/...`;
- esses recursos são requisitados na raiz do IP e retornam `404`;
- os mesmos arquivos em `/cj-catalogo/storage/...` retornam `200`;
- as imagens usam `referrerPolicy="no-referrer"`, impedindo que a rota de
  compatibilidade baseada em `Referer` identifique o projeto de origem;
- no host local `cj-catalogo.localhost`, onde a aplicação ocupa `/`, os mesmos
  recursos retornam `200`.

Esse comportamento não é específico de Laravel. Aplicações podem assumir a
raiz também em redirects, cookies, chamadas `fetch`, CSS, WebSockets, service
workers e callbacks de autenticação. Um proxy não consegue reescrever todos
esses casos de maneira genérica e segura.

## Decisão arquitetural

Aplicações web completas devem receber uma origem própria. O DevLAN adotará:

- `port` como padrão prático quando não houver DNS interno;
- `host` como experiência preferencial e isolamento completo quando houver DNS
  interno;
- `path` como modo explícito para APIs e aplicações que declarem suporte a base
  path.

O proxy continuará dividido em duas camadas:

```text
cliente da LAN
    -> Caddy no Windows (borda, firewall e TLS)
    -> Caddy no WSL (seleção do projeto)
    -> PHP-FPM, servidor dev ou arquivos estáticos
```

Essa divisão preserva o Windows como borda da máquina e mantém runtimes e
arquivos dos projetos no filesystem Linux. Não será introduzida reescrita de
HTML, JavaScript ou CSS na borda.

## Objetivos

- servir aplicações que assumem a raiz `/` sem exigir alteração de base path;
- manter cada projeto na raiz `/` de sua própria origem;
- alocar portas únicas e estáveis para projetos em modo `port`;
- manter o Firewall do Windows coerente com as portas anunciadas;
- preservar `host` como caminho de migração para um DNS interno futuro;
- tornar limitações do modo `path` visíveis na CLI e na interface;
- preservar validação, aplicação atômica e rollback dos Caddyfiles;
- manter o acesso restrito ao perfil de rede privada e à sub-rede local.

## Fora de escopo

- modificar projetos para aceitar base path;
- reescrever corpos HTML, CSS ou JavaScript no proxy;
- inferir o projeto de uma URL absoluta sem host, porta ou `Referer`;
- instalar ou administrar um servidor DNS nesta entrega;
- transformar o ambiente de desenvolvimento em uma plataforma de produção;
- executar aplicações do Ubuntu Server futuro a partir do Windows.

## Comportamento desejado

### Modo `port`

Cada projeto recebe uma porta estável na borda Windows:

```text
http://192.168.10.77:8080/ -> projeto A
http://192.168.10.77:8081/ -> projeto B
http://192.168.10.77:8082/ -> projeto C
```

O Caddy do Windows encaminha a requisição para o Caddy do WSL com a identidade
do projeto. O Caddy do WSL atende o projeto na raiz, sem adicionar ou remover
prefixo de URL.

Requisitos:

- `/storage/item.jpg` deve chegar ao `public/storage/item.jpg` do projeto;
- redirects para `/login` devem permanecer na mesma porta;
- local storage, service workers e políticas de mesma origem devem considerar a
  porta pública;
- WebSocket e HMR devem usar a mesma origem pública quando aplicável;
- dois projetos podem possuir simultaneamente `/storage/item.jpg` sem colisão;
- o funcionamento não pode depender do header `Referer`.

Portas não isolam cookies HTTP: o modelo de cookies considera hostname e path,
mas ignora a porta. Projetos servidos no mesmo IP podem colidir se usarem o
mesmo nome de cookie com `Path=/`. O DevLAN deve mostrar esse limite no modo
`port`; não deve tentar renomear cookies no proxy, pois também teria que
reescrever de forma ambígua o header `Cookie` das requisições. Aplicações que
dependem de cookies genéricos compartilhando o mesmo hostname devem usar
`host`.

### Modo `host`

Com DNS interno, todos os nomes apontam para o mesmo IP e o host seleciona o
projeto:

```text
catalogo.dev.home.arpa -> 192.168.10.77
crm.dev.home.arpa      -> 192.168.10.77
```

O domínio padrão deve ser configurável. Para uma rede doméstica ou laboratório,
preferir um subdomínio de `home.arpa`; em uma organização, preferir um
subdomínio de um domínio controlado. Evitar `.localhost`, reservado ao loopback,
e `.local`, normalmente usado por mDNS.

Além de resolver URLs absolutas, hostnames distintos isolam cookies por domínio
e devem ser considerados a solução definitiva para aplicações web completas.

O DevLAN continuará oferecendo `dns entries` e `dns sync`. Uma etapa futura
poderá exportar registros para CoreDNS, dnsmasq, AdGuard Home ou Pi-hole sem
acoplar o núcleo a um produto específico.

### Modo `path`

O modo continuará disponível, mas deixará de ser apresentado como transparente.
A CLI e a interface devem exibir um aviso:

> Requer que a aplicação suporte publicação sob um base path. URLs absolutas,
> cookies, redirects, WebSockets e service workers podem não funcionar.

A compatibilidade por `Referer` pode ser mantida para projetos existentes, mas
deve ser documentada como best effort. Ela não será usada para decidir a rota
de requisições sem `Referer`.

## Configuração e compatibilidade

### Padrão de novas instalações

Alterar `DefaultConfig().DefaultRouteMode` de `path` para `port`. O arquivo de
configuração gerado em uma instalação nova deve conter:

```toml
default_route_mode = "port"
route_base_port = 8080
route_port_count = 100
```

O pool padrão será `8080-8179`. A quantidade deve ser validada para não exceder
`65535` nem incluir as portas HTTP, HTTPS, WSL ou portas internas de runtimes.

### Instalações existentes

Não alterar silenciosamente `default_route_mode = "path"` em configurações já
existentes. Isso mudaria URLs, bookmarks e integrações sem consentimento.

Adicionar uma migração explícita:

```powershell
devlan route migrate port --dry-run
devlan route migrate port
```

O `--dry-run` deve mostrar:

- projetos afetados;
- URL atual e URL proposta;
- porta reservada;
- conflitos e portas fora do pool;
- alteração necessária no firewall;
- projetos com override que permanecerão em `path` ou `host`.

O comando sem `--dry-run` deve persistir as alocações, gerar e validar ambos os
Caddyfiles, recarregar os serviços e atualizar o firewall. Em caso de falha na
configuração dos proxies, nenhuma alteração de estado deve permanecer aplicada.

O comando existente continua válido para migração manual de um projeto:

```powershell
devlan route cj-catalogo port --port 8081
```

### Rollback operacional

O rollback explícito deve continuar simples:

```powershell
devlan route cj-catalogo path
devlan route default path
```

O DevLAN deve restaurar a configuração anterior dos proxies se a validação ou a
recarga falhar. Uma alocação de porta pode permanecer reservada no estado para
que uma troca temporária de modo não altere a URL ao retornar para `port`.

## Alocação estável de portas

### Problema atual

`EffectiveRoutePort` deriva a porta automática de `route_base_port + índice do
projeto`. Essa estratégia pode mudar a URL quando projetos são adicionados,
removidos, materializados a partir de um park ou reordenados.

### Modelo proposto

Persistir alocações separadamente da lista de projetos descobertos:

```json
{
  "route_port_allocations": {
    "/home/silver/Sites/cj-catalogo": 8081
  }
}
```

A chave deve ser o caminho normalizado do projeto. O nome pode mudar; o caminho
é a identidade já usada pelo registro e pelos parks. A estrutura não deve
materializar todos os projetos descobertos em `projects`, preservando a
separação atual entre projeto vinculado e projeto encontrado por park.

Regras do alocador:

1. respeitar `project.route_port` quando houver override explícito;
2. reutilizar a alocação persistida pelo caminho normalizado;
3. percorrer o pool em ordem crescente e escolher a primeira porta livre;
4. considerar ocupadas as portas HTTP, HTTPS, WSL, de runtime dev, overrides e
   demais alocações persistidas;
5. verificar conflito com listeners externos antes de aplicar;
6. reservar todas as portas de uma migração antes de salvar qualquer uma;
7. falhar com mensagem acionável quando o pool estiver esgotado;
8. não reciclar automaticamente alocações apenas porque um projeto ficou
   temporariamente indisponível;
9. oferecer limpeza explícita de alocações órfãs após `dry-run`:

```powershell
devlan route allocations
devlan route allocations prune --dry-run
devlan route allocations prune
```

O formato deve ser versionado e aceito pelo import/export e pelo bundle de
diagnóstico, sem expor caminhos completos nos eventos de telemetria.

## Firewall do Windows

### Problema atual

`EnsureFirewall` recebe apenas as portas HTTP/HTTPS nos fluxos de instalação,
TLS e reparo. `SetRouteMode` e `SetDefaultRouteMode` aplicam os Caddyfiles, mas
não incorporam as portas dedicadas à regra `DevLAN`. Uma rota pode funcionar na
própria máquina e continuar bloqueada para clientes da LAN.

### Política proposta

Na instalação, abrir uma faixa gerenciada e limitada para rotas `port`, além de
HTTP e HTTPS:

```text
TCP 80,443,8080-8179
perfil Private
origem LocalSubnet
```

Abrir o pool uma única vez evita exigir elevação sempre que um projeto novo for
descoberto. O Caddy só escutará as portas efetivamente atribuídas; a faixa não
deve ser liberada nos perfis Public ou Domain por padrão.

Para uma porta explícita fora do pool:

- validar o valor antes de alterar o estado;
- tentar atualizar a regra gerenciada;
- se não houver elevação, manter a configuração dos proxies, retornar warning
  destacado e marcar a postura como degradada;
- informar a porta ausente e a ação exata de reparo;
- nunca substituir silenciosamente uma regra de firewall não gerenciada.

### Refatoração

Criar uma representação de portas e faixas, por exemplo:

```go
type PortRange struct {
    From int
    To   int
}

type FirewallSpec struct {
    Ports  []int
    Ranges []PortRange
}
```

Centralizar o cálculo em uma função pura que receba a configuração efetiva. A
instalação, `route migrate`, mudanças de TLS, reparo da interface e `doctor`
devem usar a mesma especificação.

`EnsureFirewall` deve continuar idempotente, deduplicar e ordenar a saída. O
diagnóstico deve comparar a especificação desejada com a regra real, em vez de
verificar apenas a existência de uma regra chamada `DevLAN`.

## Proxy e headers

O suporte básico a `port` e `host` já existe nos renderizadores. A implementação
deve ser consolidada com testes para garantir:

- `Host` externo preservado quando necessário;
- `X-Forwarded-Host`, `X-Forwarded-Port` e `X-Forwarded-Proto` coerentes;
- remoção de headers internos enviados pelo cliente antes de adicionar valores
  controlados pelo DevLAN;
- `X-DevLAN-Project` e `X-DevLAN-Port` usados apenas entre as duas bordas;
- propagação correta de HTTPS para PHP-FPM;
- upgrade de WebSocket preservado;
- redirects não recebem prefixo no modo `port` ou `host`;
- rotas em `port` não passam pelos matchers de compatibilidade por `Referer`.

Não adicionar fallback global para `/storage`, `/img`, `/assets` ou caminhos
similares. Esses nomes são comuns e não identificam unicamente um projeto.

## CLI e interface

### CLI

Atualizar `status`, `route` e `doctor` para mostrar:

- modo efetivo e origem da configuração (`project`, `park` ou `global`);
- URL pública efetiva;
- porta persistida ou explícita;
- estado do listener no Windows;
- cobertura da porta pelo firewall;
- aviso de compatibilidade quando o modo for `path`;
- instruções de DNS quando o modo for `host` e o nome não resolver para o IP
  LAN atual.

Exemplo:

```text
cj-catalogo  port  http://192.168.10.77:8081/  listener: ok  firewall: ok
cj-crm       path  http://192.168.10.77/cj-crm/ base-path: não verificado
```

### Interface Wails

Na configuração global e por projeto:

- apresentar `Porta dedicada` como opção recomendada sem DNS;
- apresentar `Hostname` como opção recomendada quando houver DNS;
- rotular `Subcaminho` como compatibilidade dependente da aplicação;
- avisar que portas diferentes não isolam cookies do mesmo hostname;
- mostrar preview da URL antes de salvar;
- exibir conflito de porta e cobertura do firewall;
- oferecer ação de reparo quando a regra estiver degradada;
- não executar `netsh` diretamente pelo frontend; toda mutação passa por
  `internal/app.App`.

## Detecção e recomendações

Revisar `detect.RecommendRouteMode`:

- aplicações SSR, Vite/HMR e aplicações web PHP completas: recomendar `port`;
- aplicações com hostname configurado e DNS resolvendo: recomendar `host`;
- APIs ou aplicações com configuração explícita de base path: permitir
  recomendação `path`;
- Laravel não deve ser considerado compatível com subpath apenas por possuir
  `artisan` e `public/index.php`.

A detecção deve continuar passiva: ler arquivos de configuração e markers sem
executar código do projeto. O resultado é recomendação, não mudança automática.

## Segurança

- manter listeners LAN vinculados a `0.0.0.0` somente nas portas gerenciadas;
- manter firewall em `Private` + `LocalSubnet` por padrão;
- preservar allowlist, autenticação e expiração por projeto em todos os modos;
- alertar sobre colisão de cookies quando dois projetos em `port` compartilham o
  mesmo hostname;
- rejeitar headers `X-DevLAN-*` vindos da LAN antes de definir valores internos;
- não expor a API administrativa do Caddy ou o PHP-FPM à LAN;
- registrar alterações de rota, porta e firewall na auditoria;
- fazer `doctor` alertar quando a rede Windows estiver classificada como
  pública;
- não publicar certificados privados nem dados do Caddy no DNS futuro.

## Fases de implementação

### Fase 1 — Modelo e alocação

- adicionar `route_port_count` à configuração, com default e validação;
- adicionar `route_port_allocations` ao estado persistido;
- implementar alocador puro e determinístico;
- substituir o fallback baseado no índice em `EffectiveRoutePort`;
- incluir as novas estruturas em normalize, import, export e diagnóstico;
- adicionar testes de conflito, estabilidade, exaustão e projetos de park.

Saída: a URL de um projeto em modo `port` não muda quando outro projeto é
adicionado, removido ou reordenado.

### Fase 2 — Firewall gerenciado

- introduzir `FirewallSpec` com suporte a faixas;
- abrir o pool de portas durante instalação e reparo;
- comparar regra desejada e regra real no diagnóstico;
- centralizar warnings de cobertura do firewall;
- atualizar a ação de reparo da interface;
- testar geração idempotente e restrições de perfil/sub-rede.

Saída: toda porta automática do pool funciona na LAN após uma única instalação
elevada.

### Fase 3 — Contrato de proxy por origem

- completar headers encaminhados nos modos `port` e `host`;
- remover confiança em headers internos enviados pelo cliente;
- garantir raiz `/` no Caddy do WSL;
- cobrir PHP, static e dev nos dois modos;
- testar WebSocket, redirect, cookies e recursos absolutos;
- manter a compatibilidade por `Referer` isolada ao modo `path`.

Saída: aplicações que assumem a raiz funcionam sem configuração específica do
framework.

### Fase 4 — Migração e experiência de uso

- alterar o default de novas instalações para `port`;
- implementar `route migrate port --dry-run` e aplicação transacional;
- implementar inspeção e limpeza de alocações;
- atualizar `status`, `route`, `doctor`, API e interface Wails;
- acrescentar avisos claros ao modo `path`;
- documentar rollback e mudança de bookmarks.

Saída: instalações existentes migram de forma explícita, previsível e
reversível.

### Fase 5 — Preparação para DNS interno

- validar hosts e sufixos de domínio com regras consistentes;
- melhorar `dns entries` para saída legível e estruturada;
- adicionar exportação opcional para formatos comuns, sem instalar DNS;
- documentar DNS apontando para o Windows no ambiente atual e para o Ubuntu
  Server no ambiente futuro;
- testar `host` com HTTP e HTTPS na mesma porta compartilhada.

Saída: trocar o IP de destino no DNS é suficiente para mover a borda do PC de
desenvolvimento para o servidor futuro.

## Arquivos e áreas afetadas

| Área | Alterações principais |
|---|---|
| `internal/domain/model.go` | pool, alocações, resolução estável e validação |
| `internal/config/store.go` | persistência e compatibilidade de configuração |
| `internal/caddy/render.go` | contrato de headers e rotas por origem |
| `internal/platform/firewall.go` | portas, faixas, leitura e reconciliação |
| `internal/app/app.go` | aplicação transacional, migração e warnings |
| `internal/detect/js.go` | recomendações de rota conservadoras |
| `cmd/devlan/main.go` | migração, dry-run, status e diagnóstico |
| `internal/gui/app.go` | operações e estado de firewall/rota para Wails |
| `frontend/src` | seleção de modo, preview e mensagens de compatibilidade |
| `docs/CLI-AND-CONFIG.md` | comandos, defaults e migração |
| `docs/ARCHITECTURE.md` | origem própria como contrato de publicação |
| `docs/INSTALL.md` | faixa de firewall e requisito de elevação |
| `docs/OPERATIONS.md` | diagnóstico, rollback e DNS futuro |

## Estratégia de testes

### Testes unitários

- alocação estável por caminho normalizado;
- override explícito prevalece sobre alocação automática;
- portas reservadas e de runtime não são reutilizadas;
- adição e remoção de projetos não muda alocações existentes;
- projetos descobertos por park recebem alocação sem serem materializados;
- pool esgotado produz erro claro e não altera estado;
- configuração antiga sem os novos campos continua carregando;
- renderização do firewall deduplica portas e compacta faixas;
- renderizadores Caddy removem headers internos não confiáveis;
- URL efetiva contém raiz e porta/host corretos.

### Testes de integração

Criar duas aplicações-fixture que exponham os mesmos caminhos:

```text
/storage/example.jpg
/login -> redirect /dashboard
/api/origin
/ws
```

Validar em portas e hosts distintos:

- recursos absolutos retornam o conteúdo do projeto correto;
- requisições com `Referrer-Policy: no-referrer` continuam funcionando;
- local storage e service workers ficam separados por porta;
- o teste demonstra e documenta que cookies com o mesmo nome podem atravessar
  portas do mesmo hostname;
- cookies ficam isolados quando os projetos usam hostnames distintos;
- redirects preservam scheme, host e porta;
- WebSocket conecta e reconecta;
- HTTPS chega ao runtime como HTTPS;
- alteração inválida de Caddy mantém configuração e estado anteriores.

### Validação em ambiente real

- Windows com rede privada e WSL2;
- acesso no próprio Windows e em outro dispositivo da LAN;
- PHP/Laravel, Vite, static e um SSR;
- firewall ativo e inativo;
- porta automática, porta explícita e conflito externo;
- HTTP, HTTPS com CA confiável e cliente sem CA;
- DNS ausente, hosts manual e DNS interno real.

## Critérios de aceite

- uma instalação nova usa `port` como rota padrão;
- `cj-catalogo` carrega `/storage/...` pela LAN sem alteração no projeto;
- nenhuma requisição em modo `port` depende de `Referer`;
- a porta de um projeto permanece igual após reload, reboot e mudanças em outros
  projetos;
- duas aplicações podem servir o mesmo caminho absoluto sem colisão;
- a CLI e a interface deixam explícito que `port` não isola cookies por
  hostname;
- o firewall cobre todo o pool automático apenas em rede privada/local subnet;
- uma porta customizada não coberta gera aviso e reparo acionável;
- instalações existentes não mudam de URL sem migração explícita;
- `route migrate port --dry-run` não modifica arquivos, estado ou serviços;
- falha de validação/reload não deixa estado e Caddyfiles divergentes;
- `doctor` diferencia listener ausente, firewall bloqueando e DNS incorreto;
- documentação e interface não apresentam `path` como compatibilidade universal.

## Riscos e mitigação

### Muitas portas expostas

Mitigar com pool pequeno e configurável, firewall restrito a `Private` e
`LocalSubnet`, e listeners apenas para projetos ativos.

### URLs antigas deixam de funcionar

Não migrar instalações existentes silenciosamente. Exibir preview, manter
rollback e documentar alteração de bookmarks.

### Porta ocupada depois de reservada

Detectar antes da aplicação, informar o processo quando possível e permitir
override. Não trocar automaticamente uma porta persistida sem confirmação.

### Estado de firewall exige elevação

Abrir o pool durante instalação elevada. Mudanças fora do pool produzem estado
degradado visível e ação de reparo, sem esconder o problema.

### Crescimento futuro para Ubuntu Server

Manter nomes e rotas independentes do transporte Windows/WSL. No futuro, o DNS
apontará os hosts para o Ubuntu Server e o proxy nesse servidor assumirá a
borda; os projetos não precisarão mudar base path.

## Sequência recomendada de entrega

1. implementar e testar alocações persistentes;
2. implementar pool e reconciliação do firewall;
3. fechar o contrato de proxy e headers;
4. validar `cj-catalogo` e fixtures em outra máquina da LAN;
5. adicionar migração, CLI e interface;
6. mudar o default apenas para instalações novas;
7. atualizar documentação operacional;
8. preparar exportação para DNS, sem bloquear a entrega principal.

Essa ordem evita promover `port` como padrão antes de garantir estabilidade de
URL e acessibilidade real através do Firewall do Windows.
