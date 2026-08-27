# Integração WSL — Marco 7

## Resultado

O execution plane continua sendo iniciado pelo Windows através de `wsl.exe`,
mas as sondagens agora são agrupadas. O estado de configuração, portas,
processos desejados e coordenação permanece no Windows. Não existe daemon ou
segundo banco de estado no Linux.

A decisão foi registrada no [ADR 0004](adr/0004-wsl-batching.md), em conjunto
com o [ADR 0002](adr/0002-control-plane-windows.md).

## Inventário e budgets

`platform.WSLRunner` mantém um inventário agregado em memória através de
`StatsSnapshot()`. Ele registra apenas contagem, sucesso/falha,
cancelamento e duração mínima/máxima/total por operação; argumentos e
caminhos não são armazenados.

O pacote `devlan diagnostic` inclui esse snapshot como `wsl.json`, depois da
execução dos checks do `doctor`, para que a duração da coleta faça parte do
artefato de suporte sem incluir comandos ou conteúdo de projetos.

As operações observadas são `install`, `reload`, `discovery`, `status`,
`web-poll`, `doctor` e `access`. O contexto da operação é propagado para
chamadas diretas e para chamadas de Caddy/PHP.

O budget estrutural por ciclo é:

| Fluxo | Budget de spawns WSL |
| --- | --- |
| Descoberta | 1 por park (`DiscoverAllProjects`); markers Laravel em 1 chamada |
| Status de binários/sockets/processos | 1 por conjunto não vazio |
| `GET /api/v1/projects` | `parks + 2` no pior caso (discovery, sockets e dev status) |
| `GET /api/v1/status` | até 3 (PHP versionado, fallback de `php -r`, disponibilidade) |
| polling browser `GET /api/v1/overview` | até `parks + 5`; normalmente `parks + 4` |
| ACL de install/reload | 1 para todos os projetos WSL |

Install/reload também executam as ações fixas de validação/reload do Caddy WSL único;
essas ações não crescem com a quantidade de projetos. O contador real e as
durações ficam disponíveis no `WSLRunner` do processo para diagnóstico e
testes.

O benchmark reproduzível para verificar a redução de fan-out é:

```powershell
go test ./internal/platform -run '^$' -bench BenchmarkWSLBatching -benchtime=25x -count=1
```

Na medição de 26/08/2026, com runner injetado para isolar o custo do
transporte, o resultado foi:

```text
direct  16.00 spawns/op
batch    1.000 spawns/op
```

Os nanosegundos desse benchmark não representam o custo de um `wsl.exe` real;
o número de spawns é a métrica comparável. Durações de máquina devem ser
coletadas pelo inventário em uma matriz Windows/WSL de release. Para uma
medição real local existe também:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/benchmark-wsl.ps1 -Distribution Ubuntu-26.04 -Rounds 3 -Items 16
```

No host de desenvolvimento em 26/08/2026, após aquecimento, a medição real
de 16 execuções de `/bin/true` por rodada deu `direct_avg_ms=3568.35`,
`batch_avg_ms=171.42` e `speedup=20.82`. O valor inclui o overhead do
PowerShell e serve como evidência de ordem de grandeza, não como budget
universal entre máquinas.

## Batching implementado

- discovery de parks usa o scanner Linux que retorna todos os markers em uma
  sessão;
- `HasCommands` verifica todas as versões PHP, ferramentas JS e binários em
  uma sessão;
- `IsSockets` verifica todos os sockets PHP em uma sessão;
- `DevStatuses` lê todos os PID files WSL em uma sessão; listeners públicos
  continuam sendo verificados no Windows;
- ACLs de todos os projetos WSL são aplicadas em uma execução root com
  argumentos posicionais, sem interpolar caminhos no script;
- o frontend troca três requests concorrentes por `getOverview`, mantendo os
  adapters HTTP, Wails e mock sob o mesmo contrato versionado.

Quando uma operação precisa apenas de um teste direto, ela usa argumentos
diretos (`test`, `cat`, `kill`, `find`) e não cria um shell intermediário. Os
shells restantes são scripts fixos necessários para transportar uma lista de
argumentos em uma única sessão; entradas do usuário continuam posicionais.

## Contrato do execution plane

`WSLExecutionRequest` possui `version`, `requestId`, `operation`, `command`,
`asRoot` e `idempotent`. A validação rejeita versão desconhecida, request id
inválido, operação não classificada, comando vazio e argumentos com NUL.

`WSLExecutionResponse` possui `version`, `requestId`, `status`, `output`,
`error` e `errorKind`. O `requestId` idempotente deduplica chamadas
concorrentes no controlador Windows; payload diferente com o mesmo id falha
com conflito. Timeout e cancelamento respeitam o contexto e retornam erro
estruturado.

Como não há agente persistente neste desenho, handshake/reconnect de um
processo Linux separado não são necessários. Versão incompatível falha antes
da execução com `ErrExecutionProtocol`; WSL ausente/parado retorna
`WSLExecutionError` com `unavailable`; o fallback é uma nova execução direta
via `wsl.exe`.

## Matriz de falhas coberta

| Condição | Comportamento |
| --- | --- |
| binário WSL ausente | `ErrUnavailable` + categoria `unavailable` |
| distribuição ausente/parada | mensagens conhecidas de `wsl.exe` são classificadas como `unavailable` |
| WSL reiniciado | falha transitória não fica no cache; retry idempotente reexecuta |
| deadline/cancelamento | resposta `canceled` com `timeout` ou `canceled` |
| versão incompatível | `ErrExecutionProtocol`, `errorKind=version_mismatch` |
| erro de comando | `errorKind=execution`, sem alterar o estado Windows |

Os testes ficam em `internal/platform/wsl_plane_test.go` e
`internal/api/overview_test.go`.
