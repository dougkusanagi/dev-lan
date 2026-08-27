# Relatório de Análise Técnica e Arquitetural — DevLAN

> Relatório histórico. Para o estado vigente, consulte
> [STATUS.md](../../STATUS.md) e [ARCHITECTURE.md](../../ARCHITECTURE.md).

**Data:** 26 de Agosto de 2026  
**Versão do Sistema:** DevLAN Core (Marco 8 implementado / smoke real opt-in)  
**Ambiente Operacional:** Windows 11 + WSL 2 (Mirrored Networking) + Caddy + PHP-FPM / JS Dev Server

---

## 1. Visão Geral e Diagnóstico do Sistema

O **DevLAN** atingiu um nível de maturidade considerável:
- **Testes e Contratos Verificados:** Testes Go (`go test ./...`), validações de contratos de API (`contracts:check`), linter (`biome check`), TypeScript (`typecheck`) e suíte de testes do frontend (`vitest`) passam com **100% de sucesso**.
- **Arquitetura de Roteamento Baseada em Origem:** A transição dos antigos modos legados (subpaths, hostnames arbitrários) para duas origens canônicas fixas (`https://<projeto>.localhost/` local e `http(s)://<IP_LAN>:<porta>/` na rede local) simplificou o modelo mental de rede e eliminou complexidades de DNS e manipulação de `hosts`.
- **Separação Control Plane (Windows) e Execution Plane (WSL):** O Windows mantém o estado único autoritativo em `%LOCALAPPDATA%/DevLAN`, enquanto o WSL executa PHP-FPM, Caddy e processos de desenvolvimento de forma isolada.

No entanto, o crescimento do projeto através dos sucessivos marcos acumulou **arquivos monolíticos**, **estruturas transitórias de migração**, **pontos de atrito no ciclo de execução do WSL** e **alguns edge cases de rede no Windows**.

Abaixo está o detalhamento estruturado do que pode ser **simplificado**, **melhorado** e **corrigido**.

---

## 2. O que pode ser Simplificado (Simplificação)

### 2.1. Decomposição dos Arquivos Monolíticos
O projeto possui 4 componentes com concentração excessiva de responsabilidades:

| Arquivo | Tamanho Atual | Problema de Manutenibilidade | Sugestão de Divisão |
| :--- | :--- | :--- | :--- |
| `internal/app/app.go` | **2.868 linhas (94 KB)** | "God Object" que concentra regras de projeto, PHP, rotas, supervisor JS, firewall, CA, diagnósticos, backup e migração. | Quebrar em arquivos no mesmo pacote:<br>• `app.go` (struct base e lifecycle)<br>• `projects.go` (link, unlink, park, expose)<br>• `php_manager.go` (versões, pools, presets, composer)<br>• `firewall_ops.go` (reconciliação e firewall specs)<br>• `route_alloc.go` (alocações de portas e prune)<br>• `diagnostics.go` (doctor, bundle zip, audits) |
| `cmd/devlan/main.go` | **2.130 linhas (70 KB)** | Um único `switch (command)` com mais de 50 casos, flags manuais e formatação de terminal embutida. | Organizar os comandos da CLI em submódulos dentro de `cmd/devlan/` (ex: `cmd_project.go`, `cmd_php.go`, `cmd_route.go`, `cmd_topology.go`, `cmd_desktop.go`). |
| `internal/api/api.go` | **1.634 linhas (50 KB)** | Mistura configuração de servidor HTTP, middlewares de segurança (Host, CSRF, Token), SPA file serving e 25+ handlers de endpoints. | Separar em:<br>• `server.go` (lifecycle, dual-stack bind, shutdown)<br>• `middleware.go` (loopback check, CSRF, security headers)<br>• `handlers_*.go` (agrupados por domínio) |
| `frontend/src/app/AppShell.tsx` | **500+ linhas (18 KB)** | Componente concentra estado global de polling, navegação por abas, modais, shortcuts de teclado e mutações de API. | Extrair custom hooks (`useOverview`, `useProjectMutations`, `useKeyboardShortcuts`) e componentes menores de modais. |

### 2.2. Consolidação Definitiva da Borda Caddy (Conclusão do Marco 8)
- **Situação:** A topologia operacional usa somente `RenderWSLUnified` /
  `RenderSingleWSL`, com o Caddy systemd no WSL fazendo bind direto para
  `.localhost`, dashboard e portas LAN.
- **Compatibilidade:** `RenderWindows`, `RenderWSL`, `Caddyfile.windows` e os
  headers `X-DevLAN-*` permanecem apenas em fixtures e no caminho explícito de
  leitura/rollback de instalações anteriores; não são gerados pelo pipeline
  ativo.
- **Smoke:** a validação com Caddy/Windows/WSL/LAN real é opt-in e deve ser
  executada no host preparado descrito em `scripts/test-m8-real.ps1`.

### 2.3. Eliminação de Camadas Transitórias e Aliases
- `internal/app.App` expõe um único `Caddy platform.CaddyClient` operacional,
  configurado para WSL/systemd.
- Os campos `WindowsCaddy`/`WSLCaddy` e o fallback de `edgeCaddy()` são uma
  compatibilidade limitada para upgrade e testes antigos; o construtor e o
  pipeline de produção não os usam como segunda borda.

---

## 3. O que pode ser Melhorado (Melhorias de Performance, DX e Arquitetura)

### 3.1. Observabilidade e Logs em Tempo Real (Server-Sent Events / WebSocket)
- **Cenário Atual:** A CLI e a UI web fazem requisições pontuais e polling periódico para obter logs de build, logs de dev server e status de serviços.
- **Melhoria:** Adicionar um endpoint SSE (ex: `GET /api/v1/projects/:id/logs/stream`) no servidor web. Isso permite:
  - Streaming instantâneo da saída de comandos longos (`composer install`, `npm run build`, `php install`).
  - Redução expressiva de polling repetitivo contra o disco e contra o WSL.
  - Experiência de terminal responsiva e suave na GUI.

### 3.2. Otimização de Processos e Chamadas `wsl.exe`
- **Cenário Atual:** Invocar `wsl.exe` a partir do Windows tem custo de ~50ms a 150ms por processo.
- **Melhorias:**
  - **Sondagem de múltiplos manifestos em batch:** O método `projectHasManifest` em `internal/app/app.go` invoca `/usr/bin/test -f` individualmente se o path for Linux. Para dezenas de projetos estacionados (`park`), isso pode somar segundos. Uma única chamada bash agrupada (`test -f p1 && test -f p2...` ou script json) reduz a sondagem para 1 único processo.
  - **Cache inteligente de status de executáveis:** Evitar verificar `caddy version` ou `php -v` a cada ciclo de inspeção quando o estado dos binários não foi alterado.

### 3.3. Gerenciamento de Estado no Frontend (React Query / SWR)
- **Cenário Atual:** O frontend gerencia polling manual com `setInterval` e estado local em `useState`.
- **Melhoria:** Adotar um padrão declarativo com revalidação automática em foco de janela (`stale-while-revalidate`), deduplicação de requests e mutações com invalidação imediata de cache (`onSuccess -> invalidateOverview`), eliminando o delay visual perceptível após ações do usuário.

### 3.4. Melhorias de UX no Visualizador de Logs da Interface
- Adicionar suporte a **cores ANSI** (usando biblioteca leve como `ansi-to-react` ou regex rápido) para saídas do Vite, Laravel Artisan e PHP-FPM.
- Adicionar botão de auto-scroll, filtro de busca textual e alternância de quebra de linha.

---

## 4. O que deve ser Corrigido ou Endurecido (Bugs e Edge Cases)

### 4.1. [Crítico] Falha Fatal na Inicialização da API se IPv6 estiver Desabilitado no Windows
- **Local:** `internal/api/api.go` (linhas 122–126):
  ```go
  listener6, listen6Err := net.Listen("tcp", "[::1]:"+strconv.Itoa(uiPort))
  if listen6Err != nil {
      _ = listener.Close()
      return Endpoint{}, fmt.Errorf("iniciar API local IPv6 na ui_port %d: %w", uiPort, listen6Err)
  }
  ```
- **Problema:** Em máquinas Windows onde o protocolo IPv6 foi desativado nas propriedades do adaptador de rede ou por políticas corporativas de segurança (GPO), `net.Listen("tcp", "[::1]:...")` retorna erro `WSAEAFNOSUPPORT` ou `bind: cannot assign requested address`. Isso faz com que o DevLAN falhe completamente ao abrir a GUI ou a API, mesmo que o IPv4 (`127.0.0.1`) funcione perfeitamente.
- **Correção:** Tratar a falha de bind IPv6 como **aviso (warning/degraded)** e continuar a inicialização se o listener IPv4 tiver sido vinculado com sucesso.

### 4.2. Tratamento de Cold Start e Suspensão do WSL
- **Problema:** Quando o WSL entra em suspensão ou o usuário executa um comando logo após ligar a máquina, a primeira chamada a `wsl.exe` pode demorar de 2 a 5 segundos para subir a VM.
- **Correção:** 
  - Adicionar timeout adaptativo e indicação de status "Inicializando subsistema Linux..." na API/GUI em vez de disparar timeout prematuro de 500ms ou exibir erro genérico de rede.

### 4.3. Normalização de Barras e Drives Windows vs WSL em Overrides de Rota
- **Problema:** Projetos registrados por caminho (`devlan link` ou `park`) podem receber caminhos em formatos mistos: `C:\Users\dev\proj`, `c:/Users/dev/proj` ou `/mnt/c/Users/dev/proj`.
- **Correção:** Garantir que o cálculo da chave no mapa `cfg.RoutePortAllocations` passe por uma função canônica `domain.NormalizeProjectPath()` que resolva maiúsculas/minúsculas de letra de unidade (`C:` vs `c:`) e separadores de diretório antes de persistir no `state.json`.

### 4.4. Concorrência em Operações Longas (Composer / Instalação de PHP)
- **Problema:** Se duas abas da GUI ou a CLI e a GUI solicitarem simultaneamente a instalação de uma extensão ou execução do Composer, o lock de arquivo do `Store` pode causar contenção de transação ou timeout.
- **Correção:** Criar uma fila de mutações assíncronas com ID de tarefa (`task_id`) para operações longas, retornando `202 Accepted` na API e permitindo consultar o progresso.

---

## 5. Matriz de Priorização Recomendada

| Prioridade | Ação | Benefício Principal | Esforço |
| :---: | :--- | :--- | :--- |
| **P0** | **Tolerância a IPv6 desabilitado em `api.go`** | Evita crash na inicialização em ambientes corporativos | Muito Baixo (1h) |
| **P0** | **Modularização de `internal/app/app.go` e `cmd/devlan/main.go`** | Melhora radicalmente a manutenibilidade e previne bugs por acoplamento | Médio (4-6h) |
| **P1** | **Smoke e manutenção do Marco 8 (Caddy WSL único)** | Demonstra a matriz real e evita regressões | Médio (4-8h) |
| **P1** | **Batching de manifestos e checagens WSL** | Reduz tempo de carregamento do overview e `doctor` pela metade | Baixo (2-3h) |
| **P2** | **Streaming de Logs (SSE) na API e Visualizador na GUI** | Melhora expressiva na experiência do desenvolvedor (DX) | Médio (4-6h) |
| **P2** | **Refatoração do `AppShell.tsx` com Custom Hooks** | Código do frontend mais limpo, modular e fácil de testar | Baixo (2-4h) |

---

## 6. Conclusão

O núcleo funcional do DevLAN é robusto, os testes automatizados cobrem os fluxos críticos com segurança, e a decisão de usar portas LAN dedicadas com nomes `.localhost` no loopback resolveu os problemas mais complexos de roteamento e cookies.

As oportunidades mais valiosas agora estão na **modularização dos arquivos gigantes**, **resiliência do listener de rede no Windows**, **smoke real da matriz M8** e **observabilidade incremental**.
