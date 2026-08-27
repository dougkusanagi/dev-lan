# Marcos concluídos do DevLAN

## Como usar este arquivo

Este é um registro histórico, não uma tasklist. O trabalho aberto fica em
[ROADMAP.md](../ROADMAP.md). Os textos abaixo preservam critérios dos marcos
entregues; decisões vigentes estão na arquitetura e nos ADRs.

Baseline em 26/08/2026: `go test ./...`, `go vet ./...` e `npm run build`
passavam. Decisões substituídas por marcos posteriores não devem ser reescritas
retroativamente.

## Refatoração incremental da arquitetura

- [x] `R-02` Impedir persistência de senha em texto puro se o hashing do Caddy
  falhar. `SetAuth` falha fechado quando Caddy está ausente, falha ou devolve
  hash vazio. `MigrateLegacyAuth` converte todas as credenciais legadas antes
  de gravar e mantém o estado inalterado se qualquer hash falhar.

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

- [x] `M6-01` Adicionar Vitest, React Testing Library e test/coverage.
- [x] `M6-02` Testar loading/empty/error/degraded e adapters HTTP/Wails/mock.
- [x] `M6-03` Gerar ou validar tipos Go/TypeScript para impedir drift.
- [x] `M6-04` Adicionar E2E nas duas origens da GUI: projetos, URLs, override de
  porta, confirmação e rollback operacional.
- [x] `M6-05` Testar history fallback, version mismatch, sessão independente,
  API indisponível, Host/Origin inválido e CSRF.
- [x] `M6-06` Verificar acessibilidade, teclado e capturas determinísticas.
- [x] `M6-07` Integrar porta/firewall/CA sem shell no frontend.
- [x] `M6-08` Decidir manter/remover a janela Wails somente após paridade web,
  preservando tray/notificações se tiverem valor.

**Gate:** `contracts:check`, typecheck, lint, testes unitários/a11y, E2E e
testes Go cobrem contrato, segurança e fluxo crítico; qualquer regressão falha
o respectivo comando de validação.

## Marco 7 — Integração WSL madura (P1/P2)

- [x] `M7-01` Inventariar número/duração de spawns `wsl.exe` por install, reload,
  discovery, status e polling da web UI. Inventário e budgets:
  [WSL-EXECUTION-PLANE.md](../reference/WSL-EXECUTION-PLANE.md).
- [x] `M7-02` Agrupar discovery/status e remover shells quando argumentos diretos
  bastarem.
- [x] `M7-03` Definir contrato versionado/idempotente/cancelável do execution
  plane Linux, mantendo estado no Windows.
- [x] `M7-04` Avaliar agente WSL persistente com benchmark; o ganho material já
  é obtido por batching, portanto nenhum agente foi implementado.
- [x] `M7-05` Contrato direto fornece erros estruturados, cancelamento, retry e
  fallback; handshake/reconnect de agente não se aplicam sem daemon e não há
  segundo estado.
- [x] `M7-06` Testar WSL parado/ausente/reiniciado, timeout e incompatibilidade.

**Gate:** operações WSL cumprem os budgets documentados; o benchmark mediu
batching e nenhum daemon existe sem ganho adicional demonstrado.

## Marco 8 — Caddy único no WSL com rede espelhada (P0/P1)

- [x] `M8-01` Registrar ADR substituindo a fronteira de dois Caddys: exigir
  Windows 11 22H2+, WSL 2 compatível e `networkingMode=mirrored`; manter o
  control plane/estado e a API da GUI no Windows, mas mover toda a borda HTTP,
  HTTPS e LAN para um único Caddy no WSL.
- [x] `M8-02` Implementar diagnóstico de compatibilidade para versão/build do
  Windows, versão do WSL, suporte efetivo a mirrored networking, systemd,
  loopback bidirecional, acesso LAN e conflitos nas portas 80/443/pool.
- [x] `M8-03` Implementar editor transacional de `%USERPROFILE%\.wslconfig` que
  preserve seções/chaves/comentários do usuário, crie backup e configure
  `networkingMode=mirrored`, `firewall=true`, `dnsTunneling=true` e
  `autoProxy=true` sem sobrescrever o arquivo inteiro.
- [x] `M8-04` Expor a mudança por fluxo explícito de instalação/migração,
  informar que `wsl --shutdown` encerra todas as distribuições, exigir
  confirmação antes da interrupção e verificar o modo efetivo após reiniciar o
  WSL.
- [x] `M8-05` Consolidar os renderers em um Caddyfile WSL: servir
  `https://nome.localhost/`, `http(s)://IP:porta/` e
  `https://devlan.localhost/`; encaminhar apenas o dashboard para a API Windows
  em `127.0.0.1:ui_port` e manter PHP-FPM, static, Vite/SSR e assets diretamente
  no execution plane.
- [x] `M8-06` Remover do caminho operacional o protocolo intermediário entre
  Caddys, incluindo `X-DevLAN-*`, porta/admin `2019`, upstream fixo `8181`,
  renderer/artefato `Caddyfile.windows`, lifecycle e healthcheck do Caddy
  Windows; preservar forwarded headers confiáveis gerados na única borda. Os
  símbolos legados restantes são somente leitura/rollback durante upgrade.
- [x] `M8-07` Executar o Caddy WSL como serviço systemd, com bind direto em
  80/443 e apenas nas portas LAN atribuídas; implementar start, reload atômico,
  validação, healthcheck da configuração viva, restart e rollback de uma única
  instância.
- [x] `M8-08` Substituir a regra de firewall TCP tradicional por reconciliação
  conjunta e mínima do Windows Firewall e Hyper-V Firewall para
  `Private`/`LocalSubnet`, sem usar `DefaultInboundAction Allow`; cobrir 80,
  443, pool e overrides, mantendo `ui_port` restrita a loopback.
- [x] `M8-09` Mover a CA para o Caddy WSL, exportar somente o certificado raiz e
  instalá-lo no trust store do Windows; nunca copiar a chave privada para o
  host e diagnosticar emissão, validade, confiança e renovação.
- [x] `M8-10` Implementar migração segura: subir e validar o Caddy WSL antes de
  desligar/remover o Caddy Windows, detectar instalações parciais, preservar
  backup dos artefatos anteriores e oferecer rollback para a última topologia
  funcional.
- [x] `M8-11` Atualizar CLI, API, UI, doctor, repair, status, export e telemetria
  para apresentar uma única instância Caddy e o estado separado de mirrored
  networking, firewall Hyper-V, CA e serviço systemd.
- [x] `M8-12` Criar testes reais Windows+WSL para `.localhost`, dashboard,
  redirect/cookie/HTTPS, WebSocket/HMR, cada modo de projeto e cliente LAN;
  cobrir reboot, `wsl --shutdown`, VPN, troca de rede/IP, porta ocupada, firewall
  bloqueado, CA ausente e rollback da migração.
- [x] `M8-13` Remover do caminho operacional código/testes/documentação da
  topologia dupla e adicionar teste arquitetural que impeça a reintrodução de
  Caddy Windows, admin `2019` e headers de identidade internos. Artefatos
  legados permanecem apenas para detecção e rollback de instalações antigas.

**Gate:** uma instalação limpa e uma migração existente operam com exatamente
um Caddy no WSL; GUI local, projetos `.localhost` e portas LAN passam na matriz
real após reboot e `wsl --shutdown`; firewall e CA permanecem mínimos e o
rollback restaura uma topologia funcional.

**Implementação:** o contrato ativo, o fluxo de migração, o diagnóstico e os
testes determinísticos estão concluídos. O smoke Windows+WSL é opt-in em
`DEVLAN_M8_REAL=1 go test ./internal/platform -run TestM8RealWindowsWSL` ou no
script `scripts/test-m8-real.ps1`; ele deve ser executado em um host preparado
para fechar o gate após reboot, VPN/troca de IP e `wsl --shutdown`.

## Marco 9 — Observabilidade correta e performática (P1)

- [x] `M9-01` Atribuir logs ao projeto por metadado confiável, não por URI.
- [x] `M9-02` Ler arquivo ativo/rotações via streaming bounded.
- [x] `M9-03` Adicionar checkpoint/agregação incremental para polling da GUI.
- [x] `M9-04` Limitar cardinalidade e fortalecer normalização de IDs.
- [x] `M9-05` Testar logs grandes, linhas >64 KiB, rotação/truncamento/parcial.
- [x] `M9-06` Provar ausência de IP, query, headers, cookies e segredos.

**Gate:** métricas corretas respeitam budgets de CPU/memória.

## Entregas incorporadas após o Marco 9

- [x] Reutilizar reverse proxy/transporte no gateway JavaScript.
- [x] Reconciliar remoção e troca de porta sem listeners órfãos.
- [x] Tornar erros de discovery diagnósticos tipados e preservar sua causa.
- [x] Atualizar a documentação para a topologia vigente e separar fontes de
  verdade, planos e histórico.
- [x] Definir o contrato conservador do uninstall e seu manifesto versionado.
- [x] Implementar planner/dry-run e classificação estruturada do uninstall.
- [x] Preservar projetos e oferecer políticas explícitas keep/purge.
- [x] Adicionar operações longas com ID, consulta de estado e eventos SSE.

O restante foi migrado para o [roadmap vigente](../ROADMAP.md), sem duplicar
caixas abertas neste histórico.
