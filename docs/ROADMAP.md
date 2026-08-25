# Roadmap

## Status da implementação

O repositório contém a fundação Go da Fase 0, o núcleo operacional da Fase 1, o suporte WSL da Fase 1.1, o núcleo PHP completo da Fase 2, o suporte a Estáticos e JavaScript da Fase 3, as rotas e segurança da Fase 4 e a interface Wails da Fase 5. A Fase 6 está em andamento: serviço/API local, exportação/importação, diagnóstico, telemetria opt-in e preparação de updates já estão implementados.

As etapas são cumulativas. Uma fase termina somente quando seus critérios de aceite e documentação estiverem concluídos.

---

## Fase 0 — Fundação

Objetivo: estabelecer um núcleo testável antes de automatizar a máquina.

- [x] Módulo Go e estrutura por domínio/adaptadores
- [x] Modelo de configuração global + sobrescrita por projeto
- [x] Registro de `link` e `park`
- [x] Geração determinística de Caddyfiles
- [x] Executor seguro de `wsl.exe` sem shell concatenado
- [x] Testes unitários para nomes, caminhos, herança e renderização
- [x] Fixtures de projetos Laravel válidos e inválidos

**Critério de aceite:** Dada uma configuração de teste, a ferramenta gera as mesmas rotas e resolve corretamente `modo do projeto > park > global`.

---

## Fase 1 — MVP Laravel na LAN

Objetivo: publicar projetos Laravel como `http://meu-ip/nome-do-projeto` ou `https://meu-ip/nome-do-projeto`.

- [x] Instalar ou localizar Caddy no Windows e WSL
- [x] Instalar ou localizar uma versão suportada de PHP-FPM
- [x] Pool PHP-FPM compartilhado com `pm=ondemand`
- [x] Criar regra de firewall limitada à rede privada configurada
- [x] Identificar IP LAN e conflitos na porta 80
- [x] Implementar `install`, `link`, `unlink`, `park`, `unpark`
- [x] Implementar `status`, `reload`, `logs`, `open` e `doctor`
- [x] Detectar Laravel por `artisan` e `public/index.php`
- [x] Gerar rotas por subpath
- [x] Validar Caddy e PHP antes de reload
- [x] Rollback para última configuração funcional
- [x] Orientar `APP_URL`, trusted proxies e cache de configuração
- [x] Desinstalação que preserva os diretórios dos projetos
- [x] Implementar `secure` e `unsecure` como política TLS da borda Windows
- [x] Emitir certificados pela CA interna do Caddy, redirecionar HTTP para HTTPS e mostrar `SSL on/off` em `links`
- [x] Documentar a instalação da CA nos clientes da LAN

**Critérios de aceite:**
- [x] Outro dispositivo da mesma sub-rede abre dois projetos Laravel distintos.
- [x] Assets, rota normal e redirect autenticado funcionam.
- [x] Reiniciar Windows e WSL não exige refazer `link`.
- [x] Uma configuração inválida não derruba projetos já funcionais.
- [x] `doctor` identifica firewall, Caddy, PHP-FPM, socket e document root.
- [x] `secure` publica projetos na porta 443 e `unsecure` restaura HTTP.

---

## Fase 1.1 — CLI dentro do WSL

Objetivo: oferecer a mesma experiência de terminal no WSL sem criar uma segunda fonte de configuração ou um segundo controlador.

- [x] Compilar e instalar binário Linux `devlan` no WSL pelo bootstrap
- [x] Reconhecer caminhos como `~/Sites` no namespace Linux
- [x] Encaminhar operações de controle ao núcleo Windows por protocolo local versionado
- [x] Manter `%LOCALAPPDATA%/DevLAN` como fonte autoritativa do estado
- [x] Paridade para `link`, `park`, `links`, `status`, `reload`, `doctor` e `open`
- [x] Registrar explicitamente a distribuição WSL associada
- [x] Atualizar e remover o binário WSL junto com o bootstrap do Windows

**Critério de aceite:** Dentro do WSL, comandos como `devlan park ~/Sites` produzem o mesmo estado que a operação no Windows, sem duplicação de estado.

---

## Fase 2 — PHP Completo

Objetivo: suportar ambientes PHP heterogêneos com baixo consumo ocioso.

- [x] `devlan php install|list|remove` gerencia versões PHP
- [x] `devlan php use` define versão global ou sobrescrita por projeto
- [x] Registro e instalação de extensões por versão
- [x] Mestre PHP-FPM e pool compartilhado por versão registrada
- [x] `devlan php pool NAME isolated` para socket/pool isolado por projeto
- [x] Configurações `pm=ondemand`, `pm.max_children`, `pm.process_idle_timeout` e `pm.max_requests`
- [x] `composer VERSION|NAME` executa Composer com o PHP selecionado (`per-version`, `system` ou `auto`)
- [x] Presets Laravel, Symfony e PHP genérico
- [x] Separação de logs FPM por versão/pool e comando `devlan php info` sanitizado

**Critério de aceite:** Geração de múltiplas versões produz sockets e mestres FPM independentes com `pm=ondemand` e validação automatizada.

---

## Fase 3 — Estáticos e JavaScript — Implementada

Objetivo: servir builds estáticos e iniciar servidores dev sob demanda.

- [x] Implementar modos globais e por projeto: `auto`, `php`, `dev`, `static`
- [x] Detectar diretórios de build estático (`dist`, `build`, `out` e customizado)
- [x] Fallback opcional de SPA para rotas client-side
- [x] Detectar package manager (`npm`, `pnpm`, `yarn`, `bun`) via campo `packageManager` ou lockfile
- [x] Adaptadores para frameworks JS: Vite, Astro, Next.js, Nuxt, SvelteKit
- [x] Supervisor Linux no WSL para gerenciamento de processos dev
- [x] Alocação de portas estáveis e health/readiness checks
- [x] Proxy HTTP e WebSocket/HMR no Caddy
- [x] Página de inicialização para navegação HTML durante cold start
- [x] Header `Retry-After` para clientes não-interativos durante inicialização
- [x] Idle timeout com encerramento gracioso de processos dev ociosos
- [x] Comandos CLI: `devlan deps install`, `devlan build`, `devlan start`, `devlan stop`, `devlan restart`, `devlan static`, `devlan dev` e `devlan logs [NAME]`
- [x] Política de nunca instalar dependências no primeiro acesso

**Critério de aceite:** Um projeto Vite parado inicia sob demanda no primeiro acesso HTTP, entrega HMR via WebSocket e é encerrado após o período ocioso configurado; seu `dist` estático pode ser servido diretamente pelo Caddy sem processo Node ativo.

---

## Fase 4 — Rotas e Segurança — Implementada

Objetivo: atender projetos incompatíveis com subpath e reduzir exposição acidental.

- [x] Modo de rota por caminho, porta ou hostname
- [x] Recomendação automática conforme framework
- [x] Allowlist de sub-redes
- [x] Exposição temporária com expiração
- [x] Detecção de rede pública
- [x] Autenticação HTTP opcional
- [x] Distribuição assistida da CA interna e rotação de certificados
- [x] Suporte a DNS interno
- [x] Auditoria local de alterações de segurança

**Critério de aceite:** Projetos com HMR ou OAuth funcionam sem ajustes de base path, respeitando a política de allowlist e expiração configurada.

---

## Fase 5 — Interface Wails — Implementada

Objetivo: oferecer operação visual sem duplicar o núcleo.

- [x] Setup do Wails v2/v3 integrado ao núcleo Go
- [x] Frontend TypeScript + Tailwind compilado
- [x] Lista e busca em tempo real de projetos
- [x] Estados visuais: parado, iniciando, pronto, degradado e erro
- [x] Ações rápidas: abrir URL, copiar URL, start, stop, restart e visualizador de logs
- [x] Editor visual de configuração global e overrides de projeto
- [x] Diagnóstico integrado com correções guiadas
- [x] Menu na system tray com notificações acionáveis
- [x] Acessibilidade por teclado e suporte a tema claro/escuro
- [x] Empacotador e atualizador seguro

**Critério de aceite:** Todas as operações diárias podem ser executadas pela UI ou CLI com exata paridade e sem divergência de estado.

---

## Fase 6 — Operação Completa

Objetivo: tornar a ferramenta confiável para uso diário por equipes e produção local.

- [x] Serviço Windows opcional em background (independente da UI)
- [x] Inicialização no boot/login configurável
- [x] API local autenticada por token entre CLI, UI e serviço
- [ ] Instalador Windows assinado
- [ ] Atualização automática com verificação de checksum e canal estável/preview
- [x] Exportação/importação de configurações (sem credenciais/segredos)
- [x] Telemetria puramente opt-in e sanitizada
- [x] Diagnóstico exportável em arquivo único para suporte
- [x] Documentação completa de troubleshooting, recuperação e desinstalação
- [ ] Matriz de testes automatizados Windows 10/11, WSL2 e Ubuntu

O verificador de manifesto e o download em staging com SHA-256 já existem;
essa linha permanece pendente até haver um endpoint de release estável/preview,
troca automática coordenada pelo serviço e instalador Windows assinado.

**Critério de aceite:** Instalação, atualização, recuperação e remoção são previsíveis em máquinas limpas e não deixam arquivos órfãos.

---

## Definição de Completo

O produto é considerado completo quando:

- [ ] PHP, estáticos e JS funcionam nos três modos de rota aplicáveis
- [ ] Padrões globais e sobrescritas por projeto são consistentes
- [ ] CLI, Wails e serviço compartilham o mesmo domínio e modelo de dados
- [ ] Configurações possuem validação e rollback determinístico
- [ ] Exposição de rede é restrita, segura e visível
- [ ] Processos ociosos são encerrados conforme política de idle timeout
- [ ] Instalação e desinstalação não deixam firewall, serviço ou arquivos órfãos
- [ ] Testes e documentação cobrem todos os fluxos suportados
