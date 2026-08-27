# Roadmap técnico

Este arquivo contém apenas trabalho aberto. Os marcos implementados estão em
[ROADMAP-COMPLETED.md](archive/ROADMAP-COMPLETED.md); detalhes de execução ficam
em `plans/`. Prioridade: **P0** bloqueia segurança/correção, **P1** fecha uma
capacidade operacional e **P2** melhora manutenção ou escala.

## R — Refatoração incremental da arquitetura (P0/P1)

Plano: [Refatoração Go orientada a testes](plans/GO-REFACTORING.md).

- [ ] `R-01` Congelar contratos observáveis com testes de caracterização para
  CLI, HTTP, persistência, Caddy e fluxos críticos de `app.App`.
- [ ] `R-03` Dividir `internal/app`, `internal/api`, `cmd/devlan` e arquivos de
  domínio por responsabilidade sem mudar pacotes ou comportamento.
- [ ] `R-04` Substituir `any`, campos públicos de compatibilidade e estado global
  por tipos e dependências explícitas, com testes de contrato.
- [ ] `R-05` Extrair casos de uso e o reconciliador `plan → apply → verify`,
  impedindo CLI, HTTP e Wails de acessar o store ou adaptadores diretamente.
- [ ] `R-06` Separar portas de infraestrutura e adaptadores Windows/WSL/Caddy,
  incluindo testes de contrato reutilizáveis para implementações fake e reais.
- [ ] `R-07` Consolidar DTOs e geração de cliente a partir de uma especificação
  OpenAPI; registrar ADR antes de trocar a fonte do contrato atual.
- [ ] `R-08` Tornar Wails um shell HTTP fino e reorganizar a CLI por comandos;
  avaliar Cobra com ADR curto antes da adoção.
- [ ] `R-09` Criar regras de dependência/importação no CI e atualizar o mapa de
  arquitetura ao final de cada fase.

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
