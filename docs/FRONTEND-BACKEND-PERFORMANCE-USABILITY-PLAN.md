# Plano de performance e usabilidade do dashboard

## Objetivo e escopo

Este documento analisa o fluxo atual do dashboard, com foco nas mutações que
alteram TLS e HMR, e propõe uma sequência de melhorias ordenada por **menor
esforço com maior resultado percebido**. A análise é estática, realizada em
27/08/2026; os tempos reais ainda precisam ser medidos em Windows + WSL.

O objetivo não é apenas reduzir tempo de backend. A interface deve confirmar o
clique imediatamente, comunicar qual etapa está em andamento e convergir para o
estado autoritativo sem exigir `F5`.

## Resumo executivo

Os sintomas relatados são compatíveis com quatro problemas que se reforçam:

1. o frontend usa um único `busy` global, sem estado visual por operação ou por
   projeto;
2. depois da mutação, a interface espera um `GET /overview` completo e caro para
   só então atualizar a tela;
3. polling automático e refresh de mutação podem se sobrepor, e a coordenação
   atual descarta respostas pelo momento em que a requisição começou, não pela
   revisão do estado que ela representa;
4. o backend trata operações diferentes como chamadas síncronas longas e
   retorna pouco ou nenhum estado útil para reconciliação imediata.

Há ainda dois comportamentos diretamente visíveis no código:

- todos os botões de ação rápida são desabilitados quando qualquer ação está em
  curso, e o CSS aplica `cursor: wait`; isso explica o cursor de carregamento e a
  sensação de componente travado;
- o cadeado da sidebar não recebe `busy`, `disabled`, spinner nem `aria-busy`;
  portanto, a operação acontece sem feedback no próprio controle.

A melhor relação impacto/esforço é: primeiro corrigir o estado de interação no
frontend, depois tornar a reconciliação determinística, e só então reduzir o
custo do read model e introduzir eventos/tarefas assíncronas.

## Como o fluxo funciona hoje

```text
clique
  -> AppShell grava um `busy` global
  -> POST/Wails bloqueia até a operação terminar
  -> backend executa WSL/Caddy/processo
  -> frontend chama `refresh()`
  -> GET /overview refaz discovery e várias sondagens de saúde
  -> estado React é substituído por toda a fotografia
  -> `busy` global é removido
```

No intervalo entre clique e último passo, o botão não representa um estado de
domínio como `ativando`, `iniciando` ou `parando`. Ele apenas fica desabilitado
ou, no caso do cadeado, permanece visualmente inalterado.

## Achados confirmados

### 1. Estado de mutação global e impreciso no frontend

`frontend/src/app/AppShell.tsx` mantém somente `busy?: string`. `operate()` não
aceita outra ação enquanto `busy` existir, e `Overview` usa `disabled={!!busy}`
em todos os controles. Assim, iniciar HMR também bloqueia build, dependências,
remoção, PHP e porta, mesmo quando a interface deveria apenas bloquear ações
conflitantes do mesmo projeto.

Em `frontend/src/index.css`, `.quick-actions button:disabled` define
`cursor: wait`. O navegador está, portanto, obedecendo a uma decisão explícita
de estilo, não indicando necessariamente que a thread da interface travou.

O cadeado em `ProjectSidebar.tsx` não recebe qualquer estado de operação. Já o
botão de reload em `ProjectHeader.tsx` não tem loading, não participa de `busy`
e nem atualiza o overview depois de concluir.

### 2. Feedback imediato e estado transitório estão ausentes

Ao iniciar ou parar HMR, `projects` continua contendo o estado anterior até a
resposta do backend **e** o refresh completo terminarem. Embora o backend e os
tipos já conheçam `starting`, o frontend não aplica esse estado ao clique. Não
existem estados equivalentes para `stopping`, `enabling-tls` ou
`disabling-tls`.

O toast de `operate()` diz “Operação concluída” antes de `refresh()` terminar,
enquanto o componente ainda pode exibir o valor antigo. Em erro de mutação não
há reconciliação no `finally`; uma operação que foi aplicada no backend, mas
cuja resposta falhou ou excedeu timeout, deixa a interface antiga até o próximo
poll bem-sucedido ou reload manual.

### 3. Polling e refresh manual não têm um coordenador único

`AppShell` dispara polling a cada 5 segundos. A variável local `running` evita
apenas dois ticks do próprio intervalo, mas não impede concorrência entre o
polling e os `refresh()` chamados pelas mutações.

`refreshVersion` implementa “a última requisição iniciada vence”. Isso protege
contra parte das respostas fora de ordem, mas também permite que uma leitura de
background iniciada depois invalide a reconciliação foreground da mutação. Não
há abort, deduplicação compartilhada, pausa durante mutação, revisão de dados ou
política stale-while-revalidate.

### 4. Toda mutação paga por um overview amplo

Depois de TLS, HMR, PHP, firewall, porta, vínculo ou remoção, o frontend chama o
mesmo `getOverview()`. Esse read model busca projetos, estado do sistema e
versões PHP, mesmo quando somente um campo de um projeto mudou.

`internal/api/views.go` monta o overview com descoberta/configuração efetiva,
status do Caddy, sockets PHP, processos dev, versões PHP, disponibilidade WSL,
firewall, Hyper-V, compatibilidade WSL e CA. O status do Caddy é consultado na
montagem do runtime e novamente na montagem do status do sistema. O budget
documentado em `WSL-EXECUTION-PLANE.md` permite normalmente `parks + 4` spawns
WSL por poll. Portanto, um refresh de UI não é uma simples leitura em memória.

### 5. As respostas de mutação não permitem reconciliação autoritativa

O contrato TypeScript trata `saveProjectConfig`, `startDev` e `stopDev` como
`Promise<void>`. Os handlers HTTP retornam apenas mensagens. O frontend precisa
consultar toda a aplicação para descobrir o novo estado, mesmo quando o backend
acabou de calculá-lo.

Um resultado de mutação deveria carregar ao menos `operationId`, `revision`,
`project`/`projectState`, `status` e `warnings`. Isso também eliminaria a
ambiguidade entre “comando aceito”, “processo iniciando” e “processo pronto”.

### 6. Operações longas ocupam a requisição inteira

`SetProjectTLS` persiste e aplica a configuração, recarrega a infraestrutura e,
ao habilitar TLS, ainda reconcilia firewall e tenta confiar a CA. Fazer trust da
CA a cada ativação de projeto não deveria estar no caminho crítico quando a CA
já está confiada.

O start do supervisor espera a porta abrir, com limite de 30 segundos. Quando
há `DevProxy`, `StartNow` também chama o start de modo síncrono. A interface não
recebe a fase `starting` durante essa espera.

No adapter Wails, `SaveProjectConfig` tem timeout de 15 segundos,
`GetOverview` 20 segundos, `StartDev` 30 segundos e `StopDev` 15 segundos. Esses
limites ficam muito próximos do pior caso das próprias operações. Timeout não
prova que nada foi aplicado: uma fase anterior pode ter alterado estado antes
de uma fase posterior ser cancelada. Esse é um cenário importante para explicar
“funcionou, mas a tela não mudou”.

### 7. Testes não cobrem o comportamento relatado

Os testes do `AppShell` cobrem read model, loading inicial, vazio e erro. Não há
teste de integração de UI que mantenha uma mutação pendente e verifique:

- feedback em menos de um frame;
- loading somente no controle afetado;
- estado transitório e texto acessível;
- reconciliação depois de sucesso, erro, timeout e resposta fora de ordem;
- nenhuma necessidade de reload manual após TLS/start/stop.

## Ordem recomendada: menor mudança, maior ganho

| Ordem | Entrega | Esforço estimado | Impacto percebido | Motivo da ordem |
| --- | --- | ---: | ---: | --- |
| 1 | Loading local + estados transitórios | 0,5–1 dia | Muito alto | Corrige imediatamente a sensação de travamento e silêncio |
| 2 | Reconciliação confiável no frontend | 1 dia | Muito alto | Remove estados antigos e corridas entre poll e mutação |
| 3 | Resposta de mutação com estado/revisão | 1–2 dias | Muito alto | Evita refresh amplo no caminho feliz |
| 4 | Snapshot/caches do overview | 1–2 dias | Alto | Reduz latência e uso recorrente de WSL |
| 5 | Separar fases lentas de TLS e HMR | 2–4 dias | Alto | Encurta a requisição e mostra progresso real |
| 6 | Eventos/SSE e fila de operações | 3–5 dias | Médio/alto | Elimina polling como mecanismo de confirmação, mas exige mais contrato |

As estimativas servem para comparar tamanho relativo; devem ser recalibradas
após medir o host real.

## Plano de execução

### Fase 1 — feedback visual correto, sem mudar o backend

1. Trocar `busy?: string` por um mapa de operações, indexado por projeto e ação,
   por exemplo `pending[projectName].tls` e `pending[projectName].hmr`.
2. Passar o estado ao cadeado da sidebar, cabeçalho e ações do overview.
3. No clique, atualizar localmente para estados transitórios:
   `tls-enabling`, `tls-disabling`, `hmr-starting` e `hmr-stopping`.
4. Mostrar spinner pequeno e texto específico: “Ativando TLS…”, “Iniciando
   HMR…”. Em botão apenas com ícone, usar `aria-label`, `aria-busy` e tooltip
   dinâmicos.
5. Remover `cursor: wait` global. Manter `cursor: default`/`not-allowed` nos
   controles realmente indisponíveis; o spinner comunica progresso sem sugerir
   que a janela travou.
6. Desabilitar somente ações conflitantes do mesmo recurso/projeto. Busca,
   navegação, cópia de URL e outros projetos devem continuar utilizáveis.
7. Mover o toast de sucesso para depois da reconciliação. Em falha, restaurar o
   snapshot otimista e informar se o estado será verificado novamente.

**Aceite:** feedback visual em até 100 ms; exatamente um controle exibe loading;
o restante do dashboard continua interativo; leitor de tela anuncia a ação.

### Fase 2 — um único coordenador de leitura e mutação

1. Extrair `useOverview`/`useProjectMutations` de `AppShell`, sem exigir uma nova
   biblioteca inicialmente.
2. Centralizar todas as chamadas de refresh em uma fila deduplicada. Uma
   revalidação foreground pós-mutation tem prioridade sobre polling.
3. Pausar o polling enquanto houver mutação incompatível; reagendar um único
   refresh ao final em vez de criar chamadas concorrentes.
4. No HTTP, cancelar request superseded com `AbortController`. No Wails, onde o
   transporte não é abortável, ignorar respostas por revisão/epoch de dados, não
   apenas pela ordem de início.
5. Fazer revalidação no `finally` para falhas ambíguas e timeouts, com backoff
   curto limitado (por exemplo 250 ms, 750 ms, 1500 ms) até observar o estado
   esperado ou declarar falha.
6. Polling apenas com aba visível, refresh ao recuperar foco e backoff quando WSL
   estiver indisponível.

**Aceite:** nunca existem dois `getOverview()` concorrentes; uma resposta antiga
não sobrescreve estado confirmado; TLS/start/stop convergem sem `F5` inclusive
quando a resposta da mutação se perde.

### Fase 3 — mutações devolvem o novo estado

1. Criar um `MutationResult` comum no contrato HTTP/Wails:

   ```text
   operationId, operation, phase, revision, projectState, warnings
   ```

2. Fazer TLS/start/stop/restart retornarem a visão mínima e autoritativa do
   projeto. O frontend aplica esse resultado diretamente e revalida em
   background, sem bloquear o fim do loading em um overview completo.
3. Definir semântica clara de fases: `accepted`, `applying`, `starting`,
   `ready`, `stopping`, `stopped`, `failed` e `rolled_back`.
4. Usar a revisão já existente em resultados de apply para rejeitar snapshots
   anteriores. Acrescentar uma revisão/geração equivalente ao estado de
   processo, ou um `observedAt` monotônico por controller.
5. Preservar mensagens e warnings do backend; não reduzi-los a `Promise<void>`.

**Aceite:** no caminho feliz, uma mutação de projeto não depende de
`GET /overview` para atualizar seu controle; o refresh posterior é apenas uma
verificação silenciosa.

### Fase 4 — tornar o polling barato

1. Materializar uma única fotografia de Caddy/WSL por overview e reutilizá-la
   em `renderProjectViews` e `buildSystemStatusView`.
2. Separar dados por frequência:
   - quentes: estado HMR e revisão de projeto;
   - mornos: Caddy, sockets e LAN;
   - frios: versões PHP, compatibilidade WSL, firewall, Hyper-V e CA.
3. Aplicar TTL curto aos dados mornos e TTL maior aos frios, com invalidação
   explícita pela mutação que pode alterá-los. Valores iniciais sugeridos:
   2–5 s para processo/Caddy e 30–60 s para versões, compatibilidade, firewall e
   CA.
4. Criar leitura direcionada de estado do projeto, ou incluir esse estado na
   resposta da mutação, antes de adicionar mais polling.
5. Não executar health checks administrativos completos no caminho crítico de
   toda renderização. Exibir o último snapshot conhecido com idade e estado
   `verificando` quando necessário.

**Aceite:** overview quente p95 abaixo de 500 ms no host-alvo, zero spawn WSL
quando os snapshots válidos bastarem e quantidade de spawns constante em
relação ao número de componentes React.

### Fase 5 — encurtar TLS e HMR

1. Dividir TLS em fases observáveis: persistir preferência, aplicar Caddy,
   reconciliar firewall se a especificação mudou e confiar CA somente quando
   necessário/solicitado.
2. Retirar trust da CA do caminho crítico de todo toggle. A confiança é estado
   da máquina, não do projeto.
3. Para HMR, retornar `accepted/starting` assim que o supervisor assumir a
   operação e acompanhar readiness da porta em background.
4. Reservar resposta síncrona longa apenas para comandos que realmente precisam
   entregar o resultado final, como build. Para esses, expor progresso e logs.
5. Harmonizar timeouts por fase e definir idempotência por `operationId`.

**Aceite:** a API confirma aceite rapidamente; progresso continua observável;
repetir start/stop/toggle com o mesmo ID não duplica a ação; timeout nunca deixa
o cliente sem saber como consultar o resultado.

### Fase 6 — eventos depois que o contrato estiver estável

Adicionar SSE no browser e eventos Wails equivalentes para
`project-state-changed` e `operation-progress`. Polling permanece como fallback
e mecanismo de recuperação, não como confirmação primária de clique. Esta fase
vem por último porque eventos sem revisão, idempotência e reconciliação apenas
criam outra fonte de corrida.

## Instrumentação necessária

Antes e depois de cada fase, registrar sem dados sensíveis:

- `operationId`, tipo de operação e transporte HTTP/Wails;
- click-to-feedback, request total e tempo até estado confirmado;
- duração por fase: lock, persistência, apply, Caddy, firewall, CA e readiness;
- duração e quantidade de spawns WSL do overview;
- polls iniciados, deduplicados, cancelados e descartados;
- revision/observedAt recebido e aplicado pelo frontend;
- taxa de timeout, rollback e revalidação que encontrou estado diferente.

Budgets iniciais:

| Indicador | Meta |
| --- | ---: |
| Feedback visual após clique | p95 < 100 ms |
| Aplicação de resultado já retornado | p95 < 50 ms |
| Overview com snapshots quentes | p95 < 500 ms |
| Requests de overview concorrentes por cliente | 1 no máximo |
| Ações concluídas que exigem reload manual | 0 |

## Estratégia de testes

### Frontend

- usar promises controladas nos testes do `AppShell` para manter TLS/HMR
  pendentes e verificar spinner, texto, `aria-busy` e escopo do bloqueio;
- simular respostas fora de ordem entre poll e mutação;
- cobrir sucesso, rollback, timeout após commit e erro definitivo;
- usar timers falsos para polling, backoff e expiração de toast;
- adicionar Playwright para clicar em TLS/start/stop e esperar o novo estado sem
  `page.reload()`;
- manter testes de acessibilidade para todos os estados transitórios.

### Backend

- teste de contrato para `MutationResult` idêntico em HTTP e Wails;
- teste de idempotência por `operationId`;
- teste que prova invalidação seletiva dos snapshots;
- benchmark de overview frio/quente e assert estrutural de spawns WSL;
- fault injection entre persistência, apply, reload, firewall e CA;
- teste de timeout após commit, garantindo que a consulta pelo ID revele o
  resultado final correto.

### Matriz real

Validar em Windows + WSL nos cenários: WSL quente, WSL frio, Caddy parado,
projeto Vite rápido/lento, falha de porta, CA já confiada/não confiada e firewall
sem elevação. Gravar p50/p95 e não considerar a mudança concluída apenas com
mocks.

## Decisões para evitar trabalho de baixo retorno agora

- não adotar React Query/SWR apenas para substituir nomes: primeiro definir a
  política de revisão, prioridade e reconciliação; depois uma biblioteca pode
  implementar essa política;
- não iniciar por WebSocket/SSE: o ganho visual imediato vem dos estados locais
  e das respostas de mutação;
- não aumentar o polling para “parecer mais rápido”: isso amplifica o custo WSL
  e as corridas;
- não bloquear a entrega em uma grande refatoração do `AppShell`; extrair hooks
  apenas na medida necessária para centralizar o fluxo;
- não usar somente otimistic update: sempre reconciliar com revisão
  autoritativa e restaurar estado em erro/rollback.

## Definição de pronto do plano

O trabalho estará concluído quando TLS, start, stop e restart:

1. mostrarem feedback específico imediatamente;
2. não congelarem controles não relacionados;
3. exibirem estados transitórios e finais acessíveis;
4. convergirem automaticamente depois de sucesso, erro, rollback ou timeout;
5. não dependerem de reload manual;
6. tiverem latência e custo WSL medidos contra budgets;
7. possuírem cobertura de UI, contrato, concorrência e matriz Windows + WSL.

