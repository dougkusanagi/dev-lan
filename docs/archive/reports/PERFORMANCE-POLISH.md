# Relatório de performance, fluidez e polimento do DevLAN

> Snapshot da auditoria antes da consolidação documental. O trabalho aberto foi
> migrado para o [roadmap](../../ROADMAP.md).

**Data da auditoria:** 27 de agosto de 2026
**Escopo:** dashboard React/Wails, API HTTP, núcleo Go, WSL, Caddy, métricas,
testes, build e repositório.
**Natureza:** análise estática, testes automatizados e microbenchmark local. O
smoke completo Windows + WSL + Caddy + dispositivo LAN continua necessário.

## Resumo executivo

O DevLAN já tem uma base boa: o bundle do dashboard é pequeno, os contratos são
validados, a suíte automatizada passa e o trabalho local em andamento já
introduz resultados de mutação, operações assíncronas, eventos, cache do
overview e batching de sondagens WSL.

O sistema ainda parece lento porque há custos em quatro camadas diferentes:

1. **latência real:** cada `wsl.exe` é caro, o overview ainda realiza sondagens
   de sistema e métricas relê o log inteiro;
2. **latência percebida:** algumas ações ainda esperam reconciliação ampla,
   operações longas não têm progresso completo e confirmações nativas
   interrompem o fluxo;
3. **correção temporal:** timeout é tratado como rollback mesmo quando o estado
   final é ambíguo; revisão de configuração não ordena estado de processo;
4. **velocidade de engenharia:** arquivos monolíticos, testes que atravessam o
   host real e `node_modules` versionado tornam mudanças, CI e revisão mais
   lentos.

As prioridades recomendadas são:

- **P0:** corrigir semântica de timeout/rollback, listener IPv6, SSE/timeout
  HTTP, atribuição de métricas e remover `node_modules` do índice Git;
- **P1:** concluir o coordenador de estado do frontend, tornar overview
  stale-while-revalidate, mover operações longas para tarefas observáveis e
  medir WSL frio/quente;
- **P2:** ingestão incremental de métricas/logs, testes herméticos, decomposição
  dos monólitos e acabamento visual/acessível;
- **P3:** considerar um agente persistente no WSL somente se as medições após
  batching e cache ainda excederem os budgets.

## Baseline medido

| Indicador | Resultado nesta máquina | Leitura |
| --- | ---: | --- |
| Bundle JS de produção | 212,4 KB / 65,5 KB gzip | Saudável; code splitting não é prioridade |
| CSS de produção | 28,9 KB / 6,4 KB gzip | Saudável |
| Build frontend | 7,82 s | Aceitável localmente |
| Validação frontend completa | 11,90 s | 16 testes passando |
| `go test ./...` com cache parcial | 31,97 s | Alto para feedback local |
| `go vet ./...` | 1,66 s | Saudável |
| 16 spawns diretos de `wsl.exe` | 2.838,93 ms | Gargalo comprovado |
| Uma chamada WSL com 16 itens em lote | 146,04 ms | 19,44 vezes mais rápido |
| Arquivos `node_modules` rastreados | 3.328 / 56,5 MB | P0 de higiene e DX |
| Total de arquivos rastreados | 3.500 | `node_modules` representa ~95% |

O teste de corrida não foi executado localmente porque o Go desta instalação
está sem CGO. O CI Linux já possui uma etapa `go test -race ./...`.

## O que já está bem encaminhado

- Uma chamada agregada de overview substitui três leituras independentes.
- Sockets PHP e estados de processos dev são consultados em lote.
- O read model possui camadas quente (3 s) e fria (45 s), invalidadas por tipo
  de mutação.
- TLS, start, stop e restart já têm ID idempotente em memória, `202 Accepted`,
  consulta de operação e transporte por SSE/Wails.
- O frontend já mostra estados otimistas locais e suspende polling quando a aba
  está oculta.
- O contrato Go/TypeScript é gerado e validado.
- O bundle é enxuto; adicionar uma biblioteca grande apenas para cache não
  oferece boa relação custo/benefício neste momento.

Essas mudanças estão no working tree atual e devem ser estabilizadas antes de
uma segunda rodada ampla de refatoração.

## Achados priorizados

### P0 — correção e confiabilidade

#### 1. Timeout não prova rollback

`internal/api/async_operations.go` e o adapter Wails classificam
`context.DeadlineExceeded` como `rolled_back`. Uma operação pode ter persistido
ou recarregado parte do estado antes do prazo. O frontend, ao receber
`rolled_back`, restaura o snapshot antigo. Isso pode mostrar ao usuário o oposto
do estado real.

**Correção:** separar `failed`, `rolled_back` comprovado, `timed_out` e
`unknown/reconciling`. Somente restaurar otimisticamente quando o backend
confirmar compensação. Para resultado ambíguo, manter “verificando” e consultar
estado por ID/revisão até convergir.

#### 2. A API ainda falha se IPv6 loopback estiver indisponível

`internal/api/api.go` fecha o listener IPv4 quando o bind de `[::1]` falha.
Máquinas corporativas com IPv6 desativado ficam sem GUI apesar de
`127.0.0.1` estar disponível.

**Correção:** iniciar em modo degradado IPv4-only, registrar aviso acionável e
testar os três cenários: dual stack, somente IPv4 e porta ocupada.

#### 3. SSE e operações longas conflitam com `WriteTimeout`

O servidor tem `WriteTimeout` de 60 s, enquanto SSE é uma conexão longa e
instalação de dependências/PHP pode levar 2–5 minutos. Isso gera reconexões
periódicas ou respostas perdidas depois que o trabalho já começou.

**Correção:** renovar o deadline de escrita no heartbeat SSE com
`http.ResponseController`, ou separar o listener de streaming. Build,
dependências e PHP devem entrar no mesmo modelo assíncrono de operações, com
progresso e consulta autoritativa.

#### 4. Métricas não têm identidade confiável de projeto

O Caddy atual atende cada projeto na raiz `/`, identificado pelo host local ou
pela porta LAN. O agregador, porém, espera que a URI comece por
`/<projeto>/...`. Em tráfego real como `/orders/42`, a amostra é descartada ou
atribuída incorretamente. Além disso, cada consulta lê `access.jsonl` inteiro.

**Correção:** gravar um campo sanitizado e confiável `devlan_project` no log, ou
mapear host/porta a partir do snapshot de rotas. Nunca inferir projeto pela URI.
Adicionar fixture real do Caddy para origem `.localhost` e origem LAN.

#### 5. Dependências instaladas estão versionadas

Apesar de `.gitignore` conter `frontend/node_modules/`, 3.328 arquivos desse
diretório continuam no índice Git e ocupam 56,5 MB no checkout atual. Isso
aumenta clone, status, diff, antivírus, indexação e revisão.

**Correção:** em uma mudança isolada, remover o diretório apenas do índice,
preservar `package-lock.json`, validar `npm ci` em checkout limpo e decidir se o
histórico também deve ser reescrito. Reescrever histórico é opcional e exige
coordenação; retirar do índice atual já entrega o principal ganho futuro.

### P1 — caminho crítico e fluidez

#### 6. O coordenador de refresh ainda possui corridas

O refresh prioritário aborta a leitura corrente e marca uma fila, mas a limpeza
da promise e o consumo da fila acontecem em caminhos diferentes. Dependendo da
ordem das microtasks, uma leitura prioritária pode ser perdida. Além disso, uma
resposta autoritativa completa é guardada como “patch otimista” e mesclada por
cima do refresh seguinte; o snapshot novo pode ficar oculto até o próximo poll.

**Correção:** um único coordenador com estados `idle/running/queued`, prioridade
explícita e uma promise compartilhada. Patches otimistas devem conter somente
os campos alterados e expirar quando uma geração autoritativa igual ou maior for
aplicada.

#### 7. Revisão de configuração não ordena estado HMR

Start/stop/restart normalmente não alteram a revisão persistida. Duas respostas
de processo podem ter a mesma `revision`; portanto, `lastAppliedRevision` não
impede que um estado antigo sobrescreva um novo.

**Correção:** criar `runtimeGeneration` monotônica por projeto e transportar
`observedAt`/geração do controller. A ordem deve ser
`configRevision + runtimeGeneration`, e não horário de parede do browser.

#### 8. SSE atual atualiza a tela, mas não conclui a espera da ação

O evento aplica o projeto, enquanto `waitForOperation` continua fazendo GET com
backoff até observar a fase terminal. Isso duplica tráfego e mantém o controle
pendente por mais tempo do que necessário.

**Correção:** manter um registro local de promises por `operationId`; eventos
terminais resolvem a mesma promise, e polling vira fallback de recuperação.

#### 9. Cache bloqueia leitores durante a sondagem lenta

As funções `cachedHot` e `cachedCold` mantêm o mutex enquanto executam WSL,
firewall, CA e compatibilidade. Isso evita trabalho duplicado, mas coloca todas
as leituras atrás da mais lenta e não serve o último snapshot durante refresh.

**Correção:** implementar stale-while-revalidate/singleflight: retornar o
último valor imediatamente com idade, permitir apenas uma reconstrução em
background e publicar snapshot imutável de forma atômica. Falha transitória não
deve apagar o último estado conhecido.

#### 10. WSL frio ainda não tem experiência própria

O custo medido confirma que spawn domina: 16 chamadas isoladas custaram 2,84 s;
o lote equivalente, 146 ms. Quando o WSL está frio, uma única chamada ainda
pode levar segundos.

**Correção:** consolidar o restante das sondagens por ciclo, instrumentar
`cold_start`, exibir “Inicializando WSL” e usar budget adaptativo. Só avaliar um
daemon Linux persistente depois de provar que cache + batching não atingem o
budget.

### P2 — escala, manutenção e polimento

#### 11. Métricas e logs escalam com o tamanho total do arquivo

`os.ReadFile` carrega o log inteiro; o agregador cria uma cópia `string`, guarda
todas as amostras, agrupa slices por rota e ordena para percentis. Rotações não
são lidas, erro/limite do scanner não é exposto e cardinalidade não é limitada.

**Correção:** tail incremental com inode/offset, leitura das rotações dentro da
janela, buffer explícito, erro observável, top-N/buckets limitados e histogramas
bounded. Cachear snapshots por projeto+janela e invalidar por crescimento do
arquivo.

#### 12. Testes de unidade atravessam o host real

A suíte atual tenta firewall real, consulta WSL e cria/remove integração no
Menu Iniciar. Os maiores tempos observados foram `GUI_DoctorAndFix` (7,76 s),
`Phase4CLIRuns` (5,27 s), health/status da API (4,21 s) e protocolo WSL
(3,92 s). Isso torna testes lentos, ruidosos e dependentes do ambiente.

**Correção:** injetar adapters para firewall, desktop, WSL, relógio e processos.
Manter testes herméticos no caminho padrão e mover smoke real para suíte opt-in
ou job dedicado. Meta local: `go test ./...` quente abaixo de 10 s.

#### 13. Arquivos voltaram a crescer além de uma unidade revisável

| Arquivo | Linhas aproximadas |
| --- | ---: |
| `internal/app/app.go` | 3.192 |
| `cmd/devlan/main.go` | 2.190 |
| `internal/api/api.go` | 1.622 |
| `internal/domain/model.go` | 1.501 |
| `frontend/src/api.ts` | 910 |
| `frontend/src/app/AppShell.tsx` | 852 |
| `frontend/src/components/metrics/Overview.tsx` | 673 |
| `frontend/src/index.css` | 1.438 |

Isso não causa FPS baixo diretamente, mas aumenta regressões e tempo de
entendimento. A extração deve ocorrer por fronteiras já existentes, sem uma
reescrita total.

#### 14. Fricções pequenas acumulam sensação de lentidão

- toggles reversíveis como TLS usam `window.confirm`, bloqueando o fluxo;
- build/dependências não exibem fase nem logs incrementais;
- métricas iniciam uma leitura cara sempre que o projeto é selecionado;
- a UI não mostra idade do snapshot, diferenciando “parado” de “ainda não
  verificado”;
- logs não têm busca, pausa de autoscroll, wrap e virtualização.

**Correção:** remover confirmação de ações reversíveis e oferecer undo; reservar
modal para remoção/desvínculo; mostrar progresso por fase; carregar métricas sob
demanda ou após o conteúdo principal; exibir “atualizado há X s”; melhorar o
painel de logs.

## Plano de execução

### Fase 0 — baseline e P0 de correção (1–2 dias)

1. Congelar um cenário de benchmark com 1, 10 e 50 projetos; WSL frio e quente.
2. Corrigir fallback IPv4, estados `timed_out/unknown` e deadline do SSE.
3. Criar identidade confiável no access log e um teste de tráfego real por
   host/porta.
4. Retirar `frontend/node_modules` do índice em commit separado e validar
   checkout limpo com `npm ci`.
5. Registrar p50/p95 de overview, click-to-feedback, click-to-ready, número e
   duração de spawns WSL.

**Saída:** nenhuma UI mente sobre rollback; dashboard abre sem IPv6; métricas
identificam projeto; repositório limpo; baseline reproduzível.

### Fase 1 — estado determinístico no frontend (2–3 dias)

1. Extrair `useOverviewCoordinator` e `useProjectOperations`.
2. Substituir flags de fila dispersas por máquina de estados pequena e testada.
3. Adicionar `runtimeGeneration` ao contrato.
4. Fazer SSE/Wails resolver a promise da operação; polling apenas como fallback.
5. Limitar patches otimistas aos campos tocados e removê-los por geração.
6. Cobrir resposta fora de ordem, abort, SSE terminal, timeout após commit,
   rollback comprovado e troca rápida de projeto.

**Saída:** exatamente um overview em voo; nenhuma resposta velha sobrescreve
estado novo; ação concluída nunca exige F5.

### Fase 2 — read model rápido e resiliente (3–5 dias)

1. Transformar cache em stale-while-revalidate com singleflight por camada.
2. Separar snapshots: processo (1–3 s), Caddy/sockets (3–5 s), host/firewall/CA
   (30–60 s) e versões/compatibilidade (60–300 s).
3. Definir matriz de invalidação por mutação.
4. Eliminar sondagens administrativas do caminho de renderização.
5. Consolidar comandos WSL restantes e indicar cold start na interface.

**Saída:** overview quente p95 < 200 ms no backend, 0 spawn WSL em cache hit e
overview frio p95 < 1,5 s com WSL já ativo.

### Fase 3 — operações longas e observabilidade (3–5 dias)

1. Levar build, dependências, PHP e reparos para o registry de operações.
2. Modelar fases reais (`queued`, `running`, `validating`, `reloading`,
   `waiting_port`, `ready`, `failed`, `rolled_back`, `unknown`).
3. Transmitir logs incrementais sanitizados e bounded por SSE/Wails.
4. Persistir o mínimo necessário para recuperar o estado de uma operação após
   reinício, ou marcar explicitamente como interrompida/reconciliando.
5. Adicionar cancelamento somente para fases realmente canceláveis.

**Saída:** aceite da ação < 250 ms; progresso contínuo; timeout de transporte
não perde o resultado; nenhum trabalho longo fica preso a uma resposta HTTP.

### Fase 4 — métricas e logs incrementais (3–5 dias)

1. Implementar ingestão incremental com checkpoint e suporte a rotação.
2. Agregar por identidade de projeto e limitar cardinalidade/top-N.
3. Trocar percentis por estrutura bounded ou amostragem explícita.
4. Cachear por janela e atualizar somente o delta.
5. Adicionar benchmarks de 10 MB, 100 MB e linhas corrompidas/grandes.

**Saída:** consulta de métricas p95 < 150 ms após aquecimento; memória não cresce
linearmente com o histórico; resultados corretos nas duas origens.

### Fase 5 — testes e arquitetura (3–4 dias)

1. Tornar a suíte padrão hermética; mover integração real para tags/jobs.
2. Dividir `app.go` por domínio no mesmo pacote antes de criar novos packages.
3. Dividir API em server/middleware/handlers/operations/views.
4. Dividir frontend em hooks de dados, modais, configurações, runtime e
   métricas; separar CSS por componente/camada.
5. Executar race, leak/goroutine tests e perf regression tests no CI.

**Saída:** testes locais quentes < 10 s; nenhum teste padrão toca firewall,
Menu Iniciar ou WSL real; arquivos principais com responsabilidade única.

### Fase 6 — acabamento do produto (2–4 dias)

1. Trocar confirmações de ações reversíveis por atualização imediata + undo.
2. Exibir idade/qualidade do snapshot e mensagens específicas de WSL frio.
3. Adicionar skeleton apenas no primeiro load; depois manter conteúdo stale.
4. Melhorar logs com busca, wrap, autoscroll e virtualização.
5. Fazer auditoria visual em 1280×800, 1920×1080 e viewport estreito, tema
   claro/escuro e `prefers-reduced-motion`.
6. Rodar Playwright com budgets de interação e acessibilidade.

**Saída:** feedback visual p95 < 100 ms; nenhum bloqueio global para ações não
conflitantes; layout estável e navegável por teclado.

## Budgets e gates de aceite

| Indicador | Meta |
| --- | ---: |
| Feedback visual após clique | p95 < 100 ms |
| Aceite de operação assíncrona | p95 < 250 ms |
| Overview quente no backend | p95 < 200 ms |
| Overview frio com WSL ativo | p95 < 1,5 s |
| Spawns WSL em cache hit | 0 |
| Overview concorrente por cliente | máximo 1 |
| Métricas após aquecimento | p95 < 150 ms |
| Bundle inicial JS | manter < 100 KB gzip |
| `go test ./...` quente | < 10 s |
| Ações que exigem F5 | 0 |
| Estado antigo após resposta mais nova | 0 em testes de concorrência |

Cada fase deve registrar antes/depois na mesma máquina e no mesmo cenário. Uma
otimização só é considerada entregue quando melhora o budget sem degradar
correção, segurança ou acessibilidade.

## Sequência recomendada de entregas

1. PR de correções P0 e testes de regressão.
2. PR isolado de limpeza do `node_modules` rastreado.
3. PR do coordenador frontend + `runtimeGeneration`.
4. PR do cache stale-while-revalidate e batching restante.
5. PR de operações longas e logs de progresso.
6. PR de métricas incrementais.
7. PRs pequenos de decomposição e polimento visual.

Não se recomenda começar por um daemon WSL, React Query, WebSocket ou uma
reescrita da interface. O maior retorno atual vem de corrigir a semântica,
reduzir spawns, servir snapshots stale confiáveis e terminar o fluxo de
operações já iniciado.
