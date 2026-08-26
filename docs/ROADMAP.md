# Roadmap técnico do DevLAN

## Como usar este arquivo

Esta é a tasklist canônica. Detalhes ficam nos planos de
[roteamento](ORIGIN-BASED-ROUTING-PLAN.md),
[endurecimento](ENGINEERING-HARDENING-PLAN.md) e
[UI](UI-REIMPLEMENTATION-PLAN.md).

Baseline em 26/08/2026: `go test ./...`, `go vet ./...` e `npm run build`
passam. **P0** bloqueia a nova arquitetura; **P1** conclui operação segura;
**P2** melhora escala/manutenção.

## Marco 0 — Decisão e rede de segurança (P0)

- [x] `M0-01` Registrar ADR: todo projeto tem `.localhost` local e porta LAN;
  não existem modos selecionáveis.
- [x] `M0-02` Registrar ADR: control plane/estado permanecem no Windows e o WSL
  é execution plane; agente Linux persistente depende de medição.
- [x] `M0-03` Registrar ADR: browser é a GUI canônica em porta administrativa e
  `devlan.localhost`; Wails é shell opcional, não outro backend.
- [x] `M0-04` Criar fixtures PHP, static, Vite e SSR com asset absoluto,
  redirect, cookie, origem e WebSocket.
- [x] `M0-05` Testar os dois Caddys reais em portas efêmeras; testes de strings
  continuam apenas unitários.
- [x] `M0-06` Adicionar CI para Go test/vet, race compatível, frontend
  build/test e `caddy validate`.
- [x] `M0-07` Definir matriz Windows/WSL/Caddy/PHP/Node e smoke de release.

**Gate:** fixtures e baseline reproduzíveis existem antes da refatoração.

## Marco 1 — Persistência e aplicação consistentes (P0)

- [x] `M1-01` Adicionar lock entre processos e serializar mutações no processo.
- [x] `M1-02` Adicionar revisão otimista para detectar update perdido.
- [x] `M1-03` Versionar config, estado e export bundle com migrações explícitas.
- [x] `M1-04` Tornar `config.toml` + `state.json` unidade recuperável com
  manifesto/journal e fault injection.
- [x] `M1-05` Implementar `plan -> validate -> stage -> commit -> reload ->
  healthcheck -> finalize`.
- [x] `M1-06` Restaurar artefatos e recarregar Caddys/pools após falha
  pós-commit.
- [x] `M1-07` Separar bootstrap tolerante do modo operacional estrito.
- [x] `M1-08` Injetar relógio e adapters de filesystem/processo/firewall.
- [x] `M1-09` Separar domínio de transportes Wails/HTTP; toda mutação web chama
  o mesmo `internal/app.App` e o mesmo coordenador.

**Gate:** estado, artefatos e processos sempre convergem para uma revisão
completa anterior ou nova.

## Marco 2 — Remoção completa dos modos (P0)

- [x] `M2-01` Alterar o default efetivo da LAN para porta antes de apagar os
  branches antigos, mantendo testes verdes no commit.
- [x] `M2-02` Remover `RouteMode`, `DefaultRouteMode`, `RouteHost`,
  `DomainSuffix` e herança de modo em project/park/config.
- [x] `M2-03` Remover renderers `path`, `handle_path`, matchers por `Referer` e
  hostnames LAN arbitrários.
- [x] `M2-04` Remover comandos de default/migração/troca de modo e manter apenas
  inspeção/override de porta.
- [x] `M2-05` Remover opções equivalentes da API, Wails, frontend e mocks.
- [x] `M2-06` Remover `dns entries`/`dns sync` e dependências documentais caso
  não possuam outra função.
- [x] `M2-07` Remover testes antigos e adicionar teste arquitetural que impeça
  reintrodução dos símbolos/campos retirados.
- [x] `M2-08` Recriar fixtures/configs locais; não implementar migração de estado
  nunca distribuído.

**Gate:** busca no repositório não encontra API/configuração funcional de
`path`, `host` customizado ou DNS; os testes continuam passando.

## Marco 3 — Portas estáveis e firewall (P0)

- [x] `M3-01` Adicionar `route_port_count` e alocações persistidas por caminho.
- [x] `M3-02` Implementar alocador puro com reservas de borda, WSL, runtime,
  override e listeners externos.
- [x] `M3-03` Cobrir estabilidade, concorrência, conflito, exaustão, parks e
  órfãos com unit/property/fuzz tests.
- [x] `M3-04` Implementar `route allocations`, prune dry-run e override
  `--port auto|PORT`.
- [x] `M3-05` Introduzir `FirewallSpec` e adapter injetável que consulta/reconcilia
  toda a regra.
- [x] `M3-06` Identificar a regra gerenciada e preservar regras de terceiros.
- [x] `M3-07` Abrir pool só em `Private`/`LocalSubnet`, com listeners somente em
  portas atribuídas.
- [x] `M3-08` Integrar firewall ao coordenador, install, TLS, repair, doctor/UI.
- [x] `M3-09` Reservar `ui_port` fora do pool de projetos/runtimes e validar
  conflito antes de iniciar o servidor administrativo.

**Gate:** portas são estáveis e um cliente real da LAN alcança o projeto após
uma instalação elevada.

## Marco 4 — Duas origens simultâneas (P0/P1)

- [x] `M4-01` Gerar sempre `https://nome.localhost/` e `http(s)://IP:porta/` por
  projeto, sem seletor de modo.
- [x] `M4-02` Aceitar `.localhost` somente de loopback e gerenciar CA/certificado
  sem editar `hosts` ou DNS.
- [x] `M4-03` Fechar a confiança de `X-DevLAN-*` e forwarded headers entre os
  dois Caddys.
- [x] `M4-04` Garantir raiz `/`, redirect, HTTPS, WebSocket e HMR em ambas as
  origens para todas as fixtures.
- [x] `M4-05` Diagnosticar nome local, CA, porta, listener, firewall e IP LAN.
- [x] `M4-06` Mostrar as duas URLs juntas na CLI/UI; configuração expõe apenas
  porta LAN automática/customizada.
- [x] `M4-07` Explicar que cookies não são isolados por porta na URL LAN.

**Gate:** `cj-catalogo` e fixtures passam localmente e em outra máquina da LAN,
sem `Referer`, hosts file ou DNS.

## Marco 5 — Servidor da GUI web (P0/P1)

- [x] `M5-01` Adicionar `ui_port` estável/configurável (default planejado 3210)
  e bind loopback dual-stack por padrão.
- [x] `M5-02` Incorporar/servir o build da SPA com cache correto, history fallback
  e separação estrita de `/api/v1/*`.
- [x] `M5-03` Expor a mesma SPA/API em `http://127.0.0.1:ui_port/` e por reverse
  proxy em `https://devlan.localhost/`.
- [x] `M5-04` Reservar o nome `devlan` e diagnosticar porta, servidor web, Caddy,
  certificado e compatibilidade de versões.
- [x] `M5-05` Criar `DevLANClient` e adapter HTTP; Wails/tray apenas abre/embute a
  mesma superfície sem métodos de domínio duplicados.
- [x] `M5-06` Implementar Host/Origin allowlist, sessão local, CSRF, CSP, headers
  seguros e métodos HTTP sem mutação em GET.
- [x] `M5-07` Garantir que token de arquivo, senhas e segredos nunca entrem no
  bundle/DTO/local storage/URL.
- [x] `M5-08` Adicionar limites/timeouts, shutdown gracioso e progresso/logs em
  canal autenticado quando necessário.
- [x] `M5-09` Fazer `devlan gui`/tray abrir `devlan.localhost`, com fallback
  acionável para a porta.
- [x] `M5-10` Fazer `devlan gui` iniciar a interface em segundo plano e devolver
  o terminal; oferecer `devlan gui --foreground` para diagnóstico e logs.
- [x] `M5-11` Manter `ui_access=local` como default; acesso LAN só entra depois
  de autenticação, TLS, rate limiting, sessão revogável e firewall dedicados.
- [x] `M5-12` Fazer o bootstrap via `curl` instalar o DevLAN Core — CLI,
  servidor web/API e infraestrutura Windows+WSL — sem exigir o componente
  desktop nativo.
- [x] `M5-13` Implementar `devlan desktop install|status|uninstall` para instalar
  e remover independentemente um artefato Windows assinado, verificado e
  compatível com a versão principal da API.
- [x] `M5-14` Componente desktop opcional: tray, notificações, inicialização com
  login e atalho para abrir a interface web.
- [x] `M5-15` Permitir habilitar/desabilitar a inicialização com login sem
  reinstalar o Core e sem exigir que a janela Wails permaneça aberta.
- [x] `M5-16` Definir instalação por usuário, atualização atômica, rollback e
  mensagem acionável quando Desktop e Core tiverem versões incompatíveis.

**Gate:** a mesma GUI/API funciona nas duas URLs locais, nenhuma mutação é
possível por CSRF/DNS rebinding, nenhuma porta administrativa abre na LAN e o
Core funciona integralmente sem o componente desktop.

## Marco 6 — Frontend browser-first e contratos (P1)

- [ ] `M6-01` Adicionar Vitest, React Testing Library e test/coverage.
- [ ] `M6-02` Testar loading/empty/error/degraded e adapters HTTP/Wails/mock.
- [ ] `M6-03` Gerar ou validar tipos Go/TypeScript para impedir drift.
- [ ] `M6-04` Adicionar E2E nas duas origens da GUI: projetos, URLs, override de
  porta, confirmação e rollback operacional.
- [ ] `M6-05` Testar history fallback, version mismatch, sessão independente,
  API indisponível, Host/Origin inválido e CSRF.
- [ ] `M6-06` Verificar acessibilidade, teclado e capturas determinísticas.
- [ ] `M6-07` Integrar porta/firewall/CA sem shell no frontend.
- [ ] `M6-08` Decidir manter/remover a janela Wails somente após paridade web,
  preservando tray/notificações se tiverem valor.

**Gate:** build/test falha quando contrato, segurança ou fluxo crítico regride.

## Marco 7 — Integração WSL madura (P1/P2)

- [ ] `M7-01` Inventariar número/duração de spawns `wsl.exe` por install, reload,
  discovery, status e polling da web UI.
- [ ] `M7-02` Agrupar discovery/status e remover shells quando argumentos diretos
  bastarem.
- [ ] `M7-03` Definir contrato versionado/idempotente/cancelável do execution
  plane Linux, mantendo estado no Windows.
- [ ] `M7-04` Implementar agente WSL persistente somente se benchmarks mostrarem
  ganho material sobre batching.
- [ ] `M7-05` Se implementado, adicionar handshake, erros estruturados,
  restart/reconnect e fallback, sem segundo estado.
- [ ] `M7-06` Testar WSL parado/ausente/reiniciado, timeout e incompatibilidade.

**Gate:** operações WSL cumprem budget; nenhum daemon existe sem ganho medido.

## Marco 8 — Observabilidade correta e performática (P1)

- [ ] `M8-01` Atribuir logs ao projeto por metadado confiável, não por URI.
- [ ] `M8-02` Ler arquivo ativo/rotações via streaming bounded.
- [ ] `M8-03` Adicionar checkpoint/agregação incremental para polling da GUI.
- [ ] `M8-04` Limitar cardinalidade e fortalecer normalização de IDs.
- [ ] `M8-05` Testar logs grandes, linhas >64 KiB, rotação/truncamento/parcial.
- [ ] `M8-06` Provar ausência de IP, query, headers, cookies e segredos.

**Gate:** métricas corretas respeitam budgets de CPU/memória.

## Marco 9 — Robustez e documentação (P1/P2)

- [ ] `M9-01` Configurar/testar timeouts e slow clients na API web.
- [ ] `M9-02` Reutilizar reverse proxy/transporte no gateway dev.
- [ ] `M9-03` Reconciliar remoção/troca de porta sem listeners órfãos.
- [ ] `M9-04` Tornar erros de discovery diagnósticos tipados.
- [ ] `M9-05` Testar lifecycle/concorrência de serviço, web server, supervisor,
  firewall e executáveis auxiliares.
- [ ] `M9-06` Definir/medir budgets de startup, reload, polling, memória e logs.
- [ ] `M9-07` Revisar permissões de estado/auditoria e sanitização do bundle.
- [ ] `M9-08` Atualizar toda a documentação removendo subpath/host LAN/DNS e
  documentando GUI web, porta, `devlan.localhost` e postura local/LAN.
- [ ] `M9-09` Executar matriz real e anexar resultados à release.

**Gate:** documentação descreve o implementado e todos os budgets passam.
