# Estado atual

**Verificado em:** 28/08/2026
**Branch de desenvolvimento:** `refactor/r-01-r-09`

O DevLAN já funciona como um control plane Go no Windows com execution plane
WSL. Mantém projetos e configuração versionados, gera e aplica um único
Caddyfile no WSL, oferece CLI, API HTTP autenticada, dashboard React e um shell
Wails opcional.

## Implementado

- duas origens simultâneas por projeto: `.localhost` e porta LAN persistente;
- Caddy único no WSL 2 com rede espelhada, systemd e aplicação transacional;
- projetos PHP/static/JavaScript, versões PHP-FPM, Composer e gateway dev com
  cold start, readiness, HMR/WebSocket e encerramento por inatividade;
- persistência versionada com lock, revisão otimista, journal, backup e
  recuperação;
- endpoint agregado `/api/v1/overview`, cache de read model e operações longas
  assíncronas expostas por consulta e SSE;
- cache de read model com lifecycle explícito por servidor, sem registry global;
- comandos e consultas de aplicação explícitos para as fatias críticas de
  projeto, configuração, modo e read model, usados por HTTP, CLI e Wails;
- HTTP, CLI e Wails convergem nas fachadas tipadas de `application.Commands` e
  `application.Queries`; adaptadores de domínio/runtime só são compostos no
  bootstrap;
- casos de uso de recursos usam portas neutras para store, Caddy, firewall,
  trust store, rede e relógio; a composição dos adaptadores legados permanece
  concentrada em `internal/app`;
- implementações de Caddy, WSL, firewall Windows, trust store, rede e relógio
  satisfazem as portas da aplicação em `internal/platform`; `internal/app`
  apenas compõe essas implementações e mantém compatibilidade de testes;
- reconciliador real de mutações com plan/apply/verify, revisão otimista,
  journal e rollback de configuração, artefatos e runtime;
- métricas incrementais e limitadas, atribuídas por metadado confiável do Caddy,
  com suporte a rotação, truncamento e linhas grandes sem reter dados sensíveis;
- manifesto de instalação e núcleo do uninstall conservador com dry-run,
  retenção/purge e relatório estruturado;
- contratos frontend/backend verificados no build e suíte unitária/integrada.

## Em andamento

- concluir a desinstalação reversível em instalações novas e legadas;
- fechar hardening, budgets e matriz real Windows+WSL;
- refatorar a camada Go de modo incremental e orientado a testes;
- simplificar coordenação de estado e operações assíncronas no frontend.

As tarefas e critérios de aceite ficam em [ROADMAP.md](ROADMAP.md).

## Dívida conhecida

- `internal/app.App`, `internal/api` e `cmd/devlan` ainda acumulam responsabilidades;
- ainda há globais por ponteiro, DTOs duplicados e listas genéricas exigidas por
  integrações de framework;
- `application.ExtendedCommandPort` permanece transitória; testes de contrato
  reutilizáveis para adapters fake e reais seguem em R-06d;
- timeouts HTTP/SSE, permissões do estado, bundles de suporte e lifecycle
  concorrente precisam de testes adicionais;
- validação completa exige máquinas Windows+WSL reais e não deve ser simulada
  como gate concluído.

## Fontes de verdade

- comportamento: código e testes;
- estado do produto: este arquivo;
- desenho e direção: [ARCHITECTURE.md](ARCHITECTURE.md);
- trabalho aberto: [ROADMAP.md](ROADMAP.md);
- contratos de operação: `guides/` e `reference/`;
- decisões duráveis: `adr/`.
