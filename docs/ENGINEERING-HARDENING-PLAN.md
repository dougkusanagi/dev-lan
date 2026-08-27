# Plano de endurecimento de engenharia

## Escopo da revisão

Revisão estática em 26/08/2026 sobre domínio, persistência, aplicação/reload,
Caddy, firewall, API, supervisor dev, métricas, Wails e frontend. Também foram
executados com sucesso:

```text
go test ./...
go vet ./...
npm run build
```

A matriz real completa de Windows + WSL + Caddy + firewall e a carga prolongada
ainda dependem de um host preparado; o smoke opt-in foi executado neste host e
parou corretamente ao detectar que o listener LAN de teste não estava ativo.
Os achados são dívidas verificáveis ou lacunas de teste, não uma afirmação de
que todo fluxo esteja quebrado. O Marco 8 consolidou a borda em um Caddy WSL
único; referências abaixo a dois Caddys são achados históricos da revisão
anterior. A tasklist/prioridade canônicas ficam em [ROADMAP.md](ROADMAP.md).

## 1. Consistência de estado e concorrência — crítico

### Evidência anterior (resolvida no Marco 1)

- `saveAndApply` aplicava/recarregava antes de persistir a configuração;
- se `Store.Save` falha, Caddyfiles voltam em disco, mas o processo já
  recarregado não é explicitamente recarregado com o snapshot;
- falha do Caddy WSL após reload do Windows também não prova rollback do estado
  em memória do Windows;
- `Store.Save` trocava `config.toml` e `state.json` separadamente;
- CLI, API/serviço e Wails fazem load-modify-save sem lock/revisão global;
- o renderer usa `time.Now()`, prejudicando repetibilidade.

### Implementação

O coordenador de mutações usa lock entre processos, mutex interno, revisão
otimista, plan/staging, validação, commit recuperável, reload/healthcheck e
compensação. A persistência usa manifesto/journal e cópias do último par
completo; renames individuais são recuperados no próximo bootstrap.

Resultados distinguem `applied`, `degraded`, `rolled back` e `failed`.
Rollback inclui processos, não apenas arquivos.

### Testes

- fault injection após cada write, rename, validate, reload e healthcheck;
- mutações concorrentes API/CLI não perdem campos;
- crash/restart recupera uma revisão completa;
- relógio injetado gera artefatos determinísticos.

## 2. Alocação de portas e firewall — crítico

### Evidência histórica — resolvida nos Marcos 3 e 8

- alocações persistentes, parks e overrides agora têm uma política única;
- `FirewallSpec` representa portas, pool, perfil, origem e propriedades
  completas da regra;
- Windows Firewall e Hyper-V Firewall são reconciliados por adapters injetáveis;
- a política Hyper-V mantém `DefaultInboundAction=Block`, `Private` e
  `LocalSubnet`, sem abrir a `ui_port`.

### Melhoria

O estado implementado persiste alocações, centraliza reservas e usa
`FirewallSpec` puro com adapters consultáveis/reconciliáveis. A regra só é
adotada quando a assinatura gerenciada é compatível; caso contrário, há
conflito explícito. O coordenador aplica a mesma especificação em install,
reload, doctor e repair.

### Testes

- property/fuzz tests de unicidade e estabilidade;
- fake de firewall com create/update/no-op/conflict;
- Windows real em perfis Private/Public e cliente LAN;
- ordem/park/reboot não alteram URL.

## 3. Contrato e fronteira do proxy — alto

### Evidência atual

- `RenderWSLUnified` gera `.localhost`, dashboard e listeners LAN diretamente
  no único Caddy WSL;
- o renderer ativo não usa `X-DevLAN-*`, `2019` ou `8181` e remove/reconstrói
  os forwarded headers antes de encaminhar WebSocket/HMR;
- a matriz determinística cobre PHP, static, Vite/SSR, redirect, allowlist,
  auth e exposição expirada; o smoke real é opt-in por host Windows.

### Melhoria

O renderer ativo já define a matriz de headers por origem, valida nomes antes
de gerar e mantém `.localhost` local e porta como única origem LAN, conforme o
[plano de roteamento](ORIGIN-BASED-ROUTING-PLAN.md). O protocolo legado fica
restrito a fixtures e leitura/rollback de upgrades.

### Testes

- fixture real por runtime, redirect, asset, WebSocket e TLS;
- spoof de headers internos pelo cliente;
- acesso direto indevido ao listener WSL rejeitado ou inalcançável;
- nomes inválidos/reservados/duplicados falham antes de renderizar.

## 4. Métricas e logs — alto

### Evidência atual

- `GetMetrics` lê `access.jsonl` inteiro a cada chamada;
- `Aggregate` copia tudo para `string`, mantém amostras e ordena listas;
- o scanner mantém limite padrão e seu erro não é retornado;
- só o arquivo ativo é lido, apesar das rotações e janela de sete dias;
- atribuição ainda depende de identidade confiável do projeto, pois a aplicação
  ocupa a raiz `/` em ambas as origens;
- normalização cobre números/hex longos, mas não limita cardinalidade geral.

### Melhoria

Registrar identidade confiável do projeto separada da URI, fazer ingestão
streaming incremental com checkpoint, ler rotações e manter agregados bounded.
Definir limite de cardinalidade e política de percentis. Sanitização ocorre
antes da gravação.

### Testes

- logs grandes/rotacionados com memória e tempo medidos;
- linha parcial, corrompida, truncada e maior que 64 KiB;
- mesmas URIs em projetos distintos não se misturam;
- golden test exclui IP, query, header, cookie e segredo de disco/DTO.

## 5. Fronteira Windows/WSL, API web e supervisor — alto

### Evidência atual

- API usa loopback, token, comparação constante e limite de body, pontos que
  devem ser preservados;
- a API atual foi desenhada para clientes nativos com bearer token em arquivo;
  expor esse token ao JavaScript seria uma regressão de segurança;
- o núcleo Windows executa várias operações Linux através de `wsl.exe`; o
  runner já reconhece que esse spawn é relativamente caro e agrupa discovery,
  mas outras operações ainda usam chamadas e scripts separados;
- seu `http.Server` não configura timeouts/limites explícitos;
- cada request do `DevProxy` cria novo `httputil.ReverseProxy`;
- entrada existente atualiza comando/projeto/idle, mas não reconcilia mudança
  de porta/listener/backend;
- remoção durante reload não encerra explicitamente entrada antiga.

### Melhoria

Manter control plane, estado, firewall, CA e UI web no Windows; o Caddy de
borda é uma única instância systemd no WSL.
Primeiro medir e agrupar chamadas `wsl.exe`. Somente se o custo continuar
material, criar agente Linux estreito/persistente com contrato versionado; ele
executa discovery/runtimes/Caddy WSL, mas não possui estado concorrente nem
administra o Windows.

Para o browser, adicionar sessão local separada do token de arquivo, CSRF,
allowlist de Host/Origin, CSP e headers seguros. Servir a mesma API same-origin
em `127.0.0.1:ui_port` e `devlan.localhost`; bind é loopback por padrão. Acesso
LAN administrativo exige uma iniciativa explícita de autenticação/TLS/rate
limiting/firewall, não apenas trocar o bind.

Adicionar também timeouts seguros/limites de headers à API. No supervisor,
reutilizar proxy/transporte por entrada, modelar lifecycle e reconciliar o
conjunto desejado a cada revisão, preservando WebSocket e shutdown gracioso.

### Testes

- slow headers/body, cancelamento, shutdown e chamadas longas autorizadas;
- DNS rebinding, Host/Origin inválidos, CSRF, clickjacking, CSP e sessão em duas
  origens locais;
- teste prova que token persistido e segredos não chegam ao bundle/DTO/browser;
- benchmark de spawns/batching WSL e testes com distro parada/reiniciada;
- se houver agente, handshake incompatível, reconnect e operação idempotente;
- ensure idempotente, troca de porta, rename/remove e concorrência start/stop;
- `go test -race` em API, app e platform;
- benchmark e ausência de goroutine/listener órfão.

## 6. Descoberta e erros — médio

### Evidência atual

`EffectiveConfig` continua após erros de descoberta de parks, inclusive erros
que não são apenas dependência indisponível. Isso evita derrubar o reload, mas
pode ocultar configuração inválida e gerar visão efetiva incompleta.

### Melhoria

Retornar configuração junto de diagnostics tipados: `unavailable`, `invalid`,
`permission`, `timeout` e `internal`. Só categorias deliberadamente toleráveis
seguem como degraded; CLI/UI/doctor exibem origem e ação.

### Testes

Fakes por categoria, park parcialmente legível e timeout/cancelamento; nenhuma
falha desaparece ou vira apenas string solta.

## 7. Schema, parser e compatibilidade — médio

### Evidência atual

- config/estado usam versão 1 sem pipeline explícito de migrações;
- parser TOML próprio é limitado e a evolução aumenta risco de semântica
  inesperada;
- defaults na leitura exigem distinguir “campo antigo ausente” de “valor novo
  escolhido” ao trocar defaults;
- export bundle também está fixo na versão 1.

### Melhoria

Definir ownership de cada campo, versões e migrações puras. Adotar parser TOML
maduro ou formalizar gramática estrita com política de unknown keys e
round-trip. Default novo não reinterpreta arquivo antigo sem migração.

### Testes

Golden files por versão, downgrade rejeitado, unknown fields, arquivo parcial
e round-trip determinístico.

## 8. Frontend web e contratos — alto

### Evidência atual

- frontend compila e a reimplementação já começou em componentes;
- `package.json` não possui teste/dependências de teste;
- não há `*.test.*`/`*.spec.*` no frontend;
- DTOs TypeScript/Go são manuais e podem divergir;
- mocks úteis não constituem testes automatizados.

### Melhoria

Tornar o browser a superfície canônica, com `DevLANClient` e adapter HTTP.
Wails fica como shell opcional e não possui backend divergente. Adicionar
Vitest + React Testing Library, validação/geração do contrato e E2E nas origens
por porta e `devlan.localhost`. Priorizar duas URLs de projeto, override de
porta, degraded, confirmações e erros. Seguir o
[plano da UI](UI-REIMPLEMENTATION-PLAN.md) sem duplicar lógica do núcleo.

### Testes

Component tests, contract test Go/TS, E2E nas duas origens administrativas,
history fallback, incompatibilidade de versão, acessibilidade e snapshots
visuais determinísticos.

## 9. Cobertura de adapters e CI — médio

### Evidência atual

A suíte Go cobre domínio/renderização, porém firewall, serviço Windows e
executáveis auxiliares não têm testes diretos. Não foi encontrado pipeline de
CI, e os testes não sobem ainda a topologia real do Caddy WSL único com
Windows/WSL/LAN.

### Melhoria

Criar pirâmide explícita:

1. unitários puros;
2. contract tests com fakes;
3. integração com Caddy/runtimes fixture;
4. smoke Windows/WSL/LAN por release.

CI executa formatação, vet, testes, race suportado, frontend, Caddy validate e
checagem de docs. Mock Linux não substitui teste dependente de Windows.

## Ordem recomendada

1. baseline, fixtures e CI;
2. transação, lock e schema;
3. alocador e firewall;
4. remoção completa dos modos e contrato das duas origens fixas;
5. integração WSL medida e agrupada;
6. métricas corretas para as duas origens;
7. frontend e operação;
8. budgets e acabamento.

Essa ordem evita mudar o default antes que estabilidade de URL, rollback e
acesso real pela LAN sejam demonstráveis.
