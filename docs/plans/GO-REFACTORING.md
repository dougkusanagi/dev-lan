# Plano de refatoração Go orientada a testes

**Estado:** ativo
**Rastreio:** `R-01` a `R-10` em [ROADMAP.md](../ROADMAP.md)

## Objetivo

Reduzir o acoplamento do núcleo Go sem interromper os contratos existentes. A
mudança será incremental: primeiro protegemos comportamento observável, depois
separamos responsabilidades dentro dos pacotes e só então movemos fronteiras.

Não fazem parte deste plano um rewrite, troca imediata de persistência, criação
de microserviços ou adoção de framework backend completo.

## Diagnóstico do código atual

- `internal/app.App` é um god object: casos de uso, reconciliação, persistência
  e coordenação de adaptadores convivem em milhares de linhas;
- `internal/api` mistura servidor, autenticação, rotas, handlers, DTOs, cache,
  operações assíncronas e cliente;
- `cmd/devlan` faz parsing manual e acessa `config.Store` diretamente;
- Wails repete orquestração que deveria passar pelos mesmos casos de uso HTTP;
- modelos grandes e valores `any` escondem dependências e reduzem a segurança
  dos contratos;
- o cache de read model é uma dependência do `api.Server`; demais registros
  compartilhados devem seguir o mesmo lifecycle explícito.

Isso justifica a refatoração agora, mas não justifica pausar todas as entregas:
o trabalho deve ocorrer em fatias verticais curtas, começando pelos pontos que
precisam mudar no roadmap.

## Ciclo obrigatório

Para cada fatia:

1. escrever ou localizar um teste que falhe se o contrato mudar;
2. executar a suíte e registrar o baseline;
3. fazer uma única transformação estrutural;
4. executar testes focados, depois todos os gates;
5. revisar dependências e complexidade; só então seguir.

Movimentos mecânicos não alteram saída da CLI, status HTTP, JSON, schema em
disco, Caddyfile ou efeitos no host. Alterações funcionais usam outro commit e
começam com teste vermelho.

## Arquitetura alvo

O desenho completo está em [ARCHITECTURE.md](../ARCHITECTURE.md). A unidade de
deploy continua sendo um monólito modular Go, com dependências apontando para o
centro:

```text
transportes (CLI/HTTP/Wails)
        ↓
casos de uso + reconciliador
        ↓
domínio e portas
        ↑
adaptadores (store/WSL/Caddy/Windows/processos)
```

Interfaces pequenas são declaradas no pacote consumidor. DTOs de comando,
consulta, evento e erro são explícitos; modelos do domínio não são serializados
diretamente pela API.

## Fases

### 0. Rede de segurança e correção P0

- criar characterization/golden tests dos fluxos link, reload, topology,
  start/stop, overview, export/import e uninstall;
- cobrir falhas de hashing e garantir que senha bruta jamais seja persistida;
- capturar baseline de duração, alocações relevantes e `go test -race`.

### 1. Divisão segura dentro dos pacotes

- separar `app.go` em arquivos de projetos, configuração, runtime, PHP,
  topology, security e reconcile;
- separar API em server/middleware/routes e handlers por recurso;
- separar bootstrap e cada família de comandos da CLI;
- dividir modelos por agregado, sem criar pacote `utils` ou ciclos.

O pacote permanece igual nessa fase; o ganho é navegabilidade e ownership.

### 2. Dependências e casos de uso

- introduzir `application.Services` com dependências privadas e construtor;
- extrair comandos/queries e um reconciliador explícito;
- impedir acesso direto ao store nos transportes;
- substituir campos de compatibilidade e globais por objetos com lifecycle.

### 3. Contratos e transportes

- separar DTOs HTTP de domínio e remover `map[string]any`/`any` das fronteiras;
- avaliar OpenAPI 3 como fonte e `oapi-codegen`, `openapi-typescript` e
  `openapi-fetch` para reduzir contrato manual duplicado;
- organizar a CLI com `cobra` apenas se subcomandos/help/completion compensarem
  a dependência;
- fazer Wails abrir/consumir a API local, sem backend paralelo.

### 4. Portas e adaptadores

- definir portas pequenas para Store, Runner, Caddy, Firewall, TrustStore,
  Network e Clock;
- separar implementações Windows e WSL atrás dessas portas;
- criar testes de contrato e manter integrações reais opt-in.

### 5. Frontend e estado de servidor

- dividir `api.ts`, `AppShell` e telas por feature;
- centralizar operações, invalidação e geração de snapshots;
- adotar TanStack Query somente se a prova local reduzir código e eliminar
  corridas sem esconder o protocolo SSE.

### 6. Persistência opcional

Medir lock contention, recuperação e evolução de schema antes de decidir. Para
uma implementação do zero, SQLite puro Go é uma base madura para estado
operacional e arquivos continuam adequados para configuração editável e
artefatos gerados. No produto existente, qualquer migração exige ADR, migração
reversível e não bloqueia as fases anteriores.

## Dependências recomendadas

- biblioteca padrão (`net/http`, `log/slog`) como base do backend;
- bcrypt para senha, sem fallback inseguro;
- geração OpenAPI e Cobra apenas nas fases próprias;
- injeção manual, sem container DI;
- nenhuma framework web ou ORM enquanto a biblioteca padrão e SQL explícito
  mantiverem o fluxo mais simples.

## Gates

```text
go test ./...
go vet ./...
go test -race ./...
cd frontend && npm run validate && npm run build
```

Além disso, mudanças de renderização executam fixtures/Caddy real; mudanças de
host executam a matriz Windows+WSL opt-in. Uma fase termina apenas com contratos
preservados, documentação atualizada e ausência de regressão conhecida.
