# Roadmap técnico

Este arquivo contém apenas trabalho aberto. Os marcos implementados estão em
[ROADMAP-COMPLETED.md](archive/ROADMAP-COMPLETED.md); detalhes de execução ficam
em `plans/`. Prioridade: **P0** bloqueia segurança/correção, **P1** fecha uma
capacidade operacional e **P2** melhora manutenção ou escala.

## R — Refatoração incremental da arquitetura (P0/P1)

Plano: [Refatoração Go orientada a testes](plans/GO-REFACTORING.md).
Subtarefas já entregues estão registradas em
[ROADMAP-COMPLETED.md](archive/ROADMAP-COMPLETED.md) com o mesmo prefixo.

### R-01 — Contratos observáveis

- [ ] `R-01e` Caracterizar em `app.App` link/unlink, reload, topology,
  start/stop, overview, export/import e uninstall, incluindo rollback.

### R-03 — Divisão interna dos pacotes

- [ ] `R-03b` Separar as responsabilidades restantes de `internal/app/app.go`
  em arquivos focados, sem mudar pacote ou comportamento.
- [ ] `R-03c` Separar servidor, middleware, rotas e handlers de `internal/api`.
- [ ] `R-03d` Separar bootstrap e famílias de comandos de `cmd/devlan`.
- [ ] `R-03e` Dividir os modelos de domínio por agregado sem criar ciclos.

### R-04 — Tipos e lifecycle explícitos

- [ ] `R-04b` Substituir respostas e fronteiras `map[string]any`/`any` restantes
  por DTOs explícitos, preservando o JSON atual.
- [ ] `R-04c` Remover campos públicos de compatibilidade após migrar todos os
  composition roots e testes.
- [ ] `R-04d` Substituir caches e registries globais por dependências com
  lifecycle explícito e testes concorrentes.

### R-05 — Casos de uso e reconciliação

- [ ] `R-05c` Extrair comandos e consultas de aplicação das operações críticas,
  com dependências privadas e construtores explícitos.
- [ ] `R-05d` Integrar o reconciliador `plan → apply → verify` ao caminho real
  de mutação, preservando journal, rollback e revisão otimista.
- [ ] `R-05e` Fazer HTTP, CLI e o shell desktop chamarem somente casos de uso,
  sem coordenar adaptadores diretamente.

### R-06 — Portas e adaptadores

- [ ] `R-06b` Fazer os casos de uso consumirem portas pequenas para Store,
  Runner, Caddy, Firewall, TrustStore, Network e Clock.
- [ ] `R-06c` Isolar implementações Windows, WSL e Caddy atrás dessas portas.
- [ ] `R-06d` Criar testes de contrato reutilizáveis para adapters fake e reais;
  manter integrações dependentes de host como opt-in.

### R-07 — Contrato OpenAPI

- [ ] `R-07c` Completar schemas, respostas e erros no OpenAPI e torná-lo a
  única fonte do contrato HTTP.
- [ ] `R-07d` Gerar tipos/servidor Go e cliente TypeScript, removendo o manifesto
  intermediário somente após teste de paridade.

### R-08 — Dashboard e tray

- [ ] `R-08b` Criar `devlan-tray.exe` mínimo para iniciar a API, mostrar status,
  abrir o dashboard e encerrar apenas processos que ele possui.
- [ ] `R-08c` Emitir notificações acionáveis sem manter uma janela desktop.
- [ ] `R-08d` Retirar a janela Wails do caminho padrão; reavaliá-la apenas se
  minimizar para tray, atalhos ou notificações virarem prioridade central.

### R-09 — Regras arquiteturais

- [ ] `R-09b` Expandir as regras para impedir transportes acessando store ou
  adapters e adapters contendo decisões de negócio.
- [ ] `R-09c` Atualizar o mapa de arquitetura ao concluir cada fase e validar
  que ele representa o código vigente.

### R-10 — Organização da CLI

- [ ] `R-10a` Criar registro tipado de comandos, argumentos, help e dispatch.
- [ ] `R-10b` Mover cada família de comandos para arquivo focado, preservando
  saída, códigos de retorno e compatibilidade.
- [ ] `R-10c` Medir a complexidade remanescente e registrar decisão curta sobre
  adotar Cobra ou manter o parser baseado na biblioteca padrão.

**Gate:** cada fase preserva contratos, passa a suíte e reduz acoplamento ou
complexidade mensurável; nenhum big-bang ou migração de persistência é requisito.

## H — Robustez e performance (P1/P2)

- [ ] `H-01` Definir semântica de timeouts compatível com SSE e testar slow
  clients, shutdown e streams longos.
- [ ] `H-02` Testar lifecycle e concorrência de API, supervisor, firewall,
  caches e executáveis auxiliares, incluindo `go test -race ./...`.
- [ ] `H-03` Definir e medir budgets de startup, reload, overview, WSL, memória,
  logs e tamanho do frontend.
- [ ] `H-04` Revisar permissões de arquivos de estado, auditoria e sanitização
  do bundle de suporte.
- [ ] `H-05` Eliminar corridas do coordenador frontend, geração fora de ordem e
  refreshes redundantes; decidir se TanStack Query reduz complexidade suficiente.
- [ ] `H-06` Executar a matriz real de release e anexar resultados verificáveis.

## U — Desinstalação reversível (P1)

Plano: [Desinstalação reversível](plans/UNINSTALL.md).

- [ ] `U-01` Capturar backup, fingerprint e estado anterior de toda alteração
  feita por instalações novas no Windows e WSL.
- [ ] `U-02` Fechar execução transacional/idempotente e rollback do uninstall,
  removendo dependências apenas quando a proveniência provar propriedade.
- [ ] `U-03` Validar autolimpeza do executável/PATH por helper externo e relatar
  etapas que exigem reboot ou `wsl --shutdown`.
- [ ] `U-04` Migrar instalações legadas conservadoramente, sem inferir
  propriedade apenas por nome ou caminho.
- [ ] `U-05` Cobrir fault injection e matriz real: interrupção por etapa,
  segunda execução, recursos preexistentes/alterados, distro ausente, firewall
  parcial, certificado trocado, reboot e reinstalação seguida de `doctor`.

## D — Decisões posteriores (P2)

- [ ] `D-01` Medir o store atual e registrar ADR sobre manter arquivos
  transacionais ou migrar estado operacional para SQLite puro Go.
- [ ] `D-02` Reavaliar um agente Linux persistente somente se budgets reais
  mostrarem que batching e cache não atendem o produto.
