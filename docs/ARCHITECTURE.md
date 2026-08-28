# Arquitetura do DevLAN

Este documento distingue o sistema implementado da direção de refatoração. O
primeiro descreve fatos verificáveis; o segundo é o desenho que usaríamos ao
implementar o produto novamente, adotado incrementalmente e sem rewrite.

## Arquitetura atual

O DevLAN é distribuído como um executável Go no Windows, um cliente Go fino no
WSL e um frontend React. O Windows é o control plane e guarda o estado em
`%LOCALAPPDATA%/DevLAN`; o WSL 2 com rede espelhada e systemd executa Caddy,
PHP-FPM e processos dos projetos.

```text
CLI / browser / Wails / cliente WSL
                 │
        API HTTP loopback autenticada
                 │
        internal/app.App + config.Store
                 │
   ┌─────────────┼──────────────┐
 Windows      wsl.exe        geração
 firewall/CA  processos       Caddy/PHP
                 │
      Caddy único + runtimes no WSL
```

Todo projeto tem simultaneamente:

- `https://nome.localhost/`, limitado ao host;
- `http(s)://IP:porta/`, na raiz e em porta LAN persistente.

Não existem modos selecionáveis, hostname LAN ou publicação por subpath. A API
administrativa e a UI escutam loopback. O Caddy no WSL é a única borda pública;
sua API administrativa também é loopback-only.

### Pacotes e responsabilidades atuais

- `cmd/devlan`: bootstrap, parsing e execução da CLI;
- `internal/app`: casos de uso e coordenação de configuração/adaptadores;
- `internal/domain`: modelos e validações compartilhados;
- `internal/config`: configuração/estado versionados, lock, transação e journal;
- `internal/api`: servidor, autenticação, handlers, DTOs, cliente, read model,
  cache, operações assíncronas e SSE;
- `internal/platform`: processos, rede, WSL, firewall, CA e gateway JavaScript;
- `internal/caddy`, `internal/php`, `internal/detect`, `internal/metrics`: geração
  e capacidades especializadas;
- `internal/gui`: bindings Wails;
- `frontend`: dashboard React e cliente HTTP.

`internal/app.App` expõe somente `Caddy`, a dependência da borda WSL única;
clientes Caddy legados ficam privados ao pacote e só participam de migração ou
rollback explícitos.

O fluxo de mutação converge em persistir a intenção e reconciliar recursos por
`plan → validate → stage → commit → reload → healthcheck`, com recuperação por
journal/backup. Operações longas possuem IDs, estado consultável e eventos SSE.
O overview agrega projetos, runtime, PHP e topologia para reduzir travessias WSL.

### Estado e artefatos

```text
%LOCALAPPDATA%/DevLAN/
  config.toml
  state.json
  installation-manifest.json
  api-endpoint.json
  wsl-distribution
  generated/
  logs/

/etc/caddy/Caddyfile
/etc/devlan/
  generated/php/
  backups/
```

TOML contém preferências editáveis; JSON contém estado e manifesto versionados.
Arquivos em `generated/` e a cópia viva do Caddy são derivados e não devem ser
editados manualmente. Detalhes estão em
[Persistência](reference/PERSISTENCE.md) e
[Execution plane WSL](reference/WSL-EXECUTION-PLANE.md).

### Limitações estruturais atuais

Os fluxos funcionam, mas `app.App`, API e CLI ainda concentram responsabilidades,
embora as implementações de `internal/app`, `internal/api`, `cmd/devlan` e
`internal/domain` já estejam divididas por assunto. Há
acesso de transportes ao store, DTOs duplicados, registros genéricos de
integrações de framework e orquestração repetida no Wails. O cache de read model
é criado e encerrado pelo `api.Server`, mas ainda há pontos que dificultam testes
de lifecycle e fazem uma
mudança atravessar arquivos grandes. A solução é evolução incremental orientada
por testes, não uma troca de stack.

## Arquitetura alvo

Para uma implementação nova, manteríamos um **monólito modular** Go: uma unidade
de deploy, limites internos claros, ports and adapters e CQRS leve apenas para
distinguir comandos de consultas. Microserviços adicionariam falhas distribuídas
sem resolver um problema real deste produto local.

```text
cmd/
  devlan/                 composição e CLI
  devlan-wsl/             cliente Linux fino
internal/
  domain/
    project/              agregados, value objects e regras
    topology/
    runtime/
    installation/
  application/
    command/              casos de uso de escrita
    query/                overview/status/metrics
    reconcile/            plano, aplicação, verificação e rollback
    ports/                interfaces pequenas consumidas pela aplicação
  adapters/
    store/                arquivo hoje; SQLite se decidido por ADR
    wsl/                  transporte estruturado por wsl.exe
    caddy/                render, validate, publish e health
    windows/              firewall, trust store, PATH e self-remove
    process/              supervisor e gateway JavaScript
  transport/
    http/                 rotas, middleware e DTOs
    cli/                  comandos e apresentação
    wails/                shell fino sobre HTTP
  observability/          slog, métricas e support bundle
api/
  openapi.yaml            contrato externo, quando R-07 for aceito
frontend/src/
  app/                    composição, router e providers
  features/               projects, topology, php, metrics, settings
  shared/                 cliente gerado, UI e utilitários sem regra de negócio
```

Pastas são consequência dos limites, não objetivo isolado. Durante a migração,
podemos manter os pacotes existentes e chegar a esse desenho por fatias.

### Regras de dependência

1. `domain` usa apenas biblioteca padrão e não conhece transporte ou storage.
2. `application` depende do domínio e de interfaces declaradas pelo consumidor.
3. `adapters` e `transport` apontam para dentro; nunca um para o outro por
   acesso concreto.
4. Somente `cmd` compõe implementações e controla startup/shutdown.
5. CLI, HTTP e Wails executam os mesmos casos de uso.
6. Eventos de domínio/aplicação não carregam objetos HTTP nem detalhes WSL.

### Interfaces e DTOs

Interfaces devem representar uma necessidade pequena, por exemplo
`ProjectRepository`, `PlanPublisher`, `CommandRunner`, `TrustStore`, `Firewall`,
`Clock` e `OperationSink`. Elas ficam próximas do caso de uso, não em um pacote
genérico de interfaces.

Os contratos se dividem em:

- **commands**: intenção validada e identificador idempotente;
- **queries/views**: snapshots imutáveis para CLI/UI;
- **events**: progresso e invalidação com versão/generation;
- **transport DTOs**: JSON e códigos de erro estáveis;
- **persistence records**: schema versionado, convertido para o domínio.

Isso evita serializar agregados diretamente e remove `map[string]any` das
fronteiras. Uma especificação OpenAPI pode gerar servidor/tipos Go e cliente
TypeScript, mas sua adoção exige ADR e migração do contrato verificado atual.

### Escolhas de implementação

- `net/http` e o ServeMux moderno são suficientes; não usaríamos framework web;
- injeção manual no composition root evita container e dependências mágicas;
- `log/slog` fornece logging estruturado;
- bcrypt continua sendo a primitiva de senha, sempre fail-closed;
- Cobra é apropriado quando a CLI for dividida, por help, subcomandos e
  completion, mas deve substituir parsing manual em uma fase isolada;
- OpenAPI com `oapi-codegen`, `openapi-typescript` e `openapi-fetch` reduz a
  duplicação de DTOs se adotado como única fonte;
- para um produto novo, SQLite puro Go seria uma boa base para estado
  operacional, mantendo TOML para preferências e arquivos para artefatos. No
  produto atual, essa troca só ocorre após medição e ADR;
- TanStack Query é candidato para server state do frontend, após prova de que
  simplifica SSE, invalidação e operações sem criar duas fontes de verdade.

### Fluxo alvo de uma mutação

```text
transport → parse/validate DTO → command handler → domínio
          → plano persistível → adapters.apply → verify
          → commit/recovery → event + view invalidation
```

Todos os passos recebem `context.Context`, deadline e operation ID. Rollback e
reexecução são explícitos. Leituras usam snapshots versionados e não iniciam
efeitos colaterais inesperados.

## Evolução

A sequência e os gates estão no
[plano de refatoração](plans/GO-REFACTORING.md). Decisões que mudem topologia,
persistência ou fonte dos contratos recebem ADR. O roadmap permanece a única
tasklist; este documento descreve direção, não progresso.
