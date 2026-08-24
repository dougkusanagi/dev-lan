# Roadmap

## Status da implementação

O repositório contém a fundação Go da Fase 0 e o núcleo operacional da Fase 1:

- registro, herança, detecção Laravel e fixtures unitárias;
- geração determinística e aplicação segura com rollback;
- CLI de registro, operação e diagnóstico;
- bootstrap reproduzível via `curl`/PowerShell para Go, WSL/Ubuntu, PHP-FPM,
  Composer, Caddy e a CLI compilada;
- adaptadores sem shell concatenado para `wsl.exe`, Caddy, navegador e
  firewall Windows.

O bootstrap instala os runtimes e prepara os serviços, mas o aceite ponta a
ponta em outro dispositivo ainda precisa ser executado em uma máquina limpa.
`devlan install` continua sendo a etapa idempotente do núcleo e não instala
dependências dos projetos.

As etapas são cumulativas. Uma fase termina somente quando seus critérios de aceite e documentação estiverem concluídos.

## Fase 0 — fundação

Objetivo: estabelecer um núcleo testável antes de automatizar a máquina.

- módulo Go e estrutura por domínio/adaptadores;
- modelo de configuração global + sobrescrita por projeto;
- registro de `link` e `park`;
- geração determinística de Caddyfiles;
- executor seguro de `wsl.exe` sem shell concatenado;
- testes unitários para nomes, caminhos, herança e renderização;
- fixtures de projetos Laravel válidos e inválidos.

Critério de aceite: dada uma configuração de teste, a ferramenta gera as mesmas rotas e resolve corretamente `modo do projeto > park > global`.

## Fase 1 — MVP Laravel na LAN

Objetivo: publicar projetos Laravel como `http://meu-ip/nome-do-projeto`.

- instalar ou localizar Caddy no Windows e WSL;
- instalar ou localizar uma versão suportada de PHP-FPM;
- pool PHP-FPM compartilhado com `pm=ondemand`;
- criar regra de firewall limitada à rede privada configurada;
- identificar IP LAN e conflitos na porta 80;
- implementar `install`, `link`, `unlink`, `park`, `unpark`;
- implementar `status`, `reload`, `logs`, `open` e `doctor`;
- detectar Laravel por `artisan` e `public/index.php`;
- gerar rotas por subpath;
- validar Caddy e PHP antes de reload;
- rollback para última configuração funcional;
- orientar `APP_URL`, trusted proxies e cache de configuração;
- desinstalação que preserva os diretórios dos projetos.

Critérios de aceite:

- outro dispositivo da mesma sub-rede abre dois projetos Laravel distintos;
- assets, rota normal e redirect autenticado são testados;
- reiniciar Windows e WSL não exige refazer `link`;
- uma configuração inválida não derruba projetos já funcionais;
- `doctor` identifica firewall, Caddy, PHP-FPM, socket e document root.

## Fase 2 — PHP completo

Objetivo: suportar ambientes PHP heterogêneos com baixo consumo ocioso.

- instalar/listar/remover versões PHP;
- versão global e sobrescrita por projeto;
- extensões por versão;
- pools compartilhados por versão;
- pool isolado opcional por projeto;
- `pm=ondemand`, limites e idle timeout configuráveis;
- Composer por versão/ambiente;
- presets Laravel, Symfony e PHP genérico;
- logs separados e página de informações sanitizada.

Critério de aceite: projetos em duas versões de PHP funcionam simultaneamente e nenhum worker permanece ocioso após o timeout.

## Fase 3 — estáticos e JavaScript

Objetivo: servir builds estáticos e iniciar servidores dev sob demanda.

- implementar modos globais e por projeto: `auto`, `php`, `dev`, `static`;
- detectar `dist`, `build`, `out` e saída configurada;
- fallback de SPA opcional;
- detectar package manager por `packageManager` e lockfile;
- adaptadores Vite, Astro, Next.js, Nuxt e SvelteKit;
- supervisor Linux para processos dev;
- portas estáveis e health/readiness checks;
- proxy HTTP e WebSocket/HMR;
- página de inicialização para navegação HTML;
- `Retry-After` para clientes não interativos;
- idle timeout e encerramento gracioso;
- `deps install`, `build`, `start`, `stop`, `restart` e logs;
- nunca instalar dependências no primeiro acesso.

Critério de aceite: um projeto Vite parado inicia no primeiro acesso, entrega HMR e é encerrado depois do período ocioso; seu `dist` também pode ser servido sem Node ativo.

## Fase 4 — rotas e segurança

Objetivo: atender projetos incompatíveis com subpath e reduzir exposição acidental.

- modo por caminho, porta ou hostname;
- recomendação automática conforme framework;
- allowlist de sub-redes;
- exposição temporária com expiração;
- detecção de rede pública;
- autenticação opcional;
- HTTPS por CA interna;
- suporte posterior a DNS interno;
- auditoria local de alterações relevantes.

Critério de aceite: projetos com HMR ou OAuth podem usar porta/host sem ajustes de base path, e nenhuma rota permanece exposta além da política configurada.

## Fase 5 — interface Wails

Objetivo: oferecer operação visual sem duplicar o núcleo.

- fixar e validar uma versão do Wails;
- frontend TypeScript + Tailwind compilado;
- lista e busca de projetos;
- estados: parado, iniciando, pronto, degradado e erro;
- abrir/copiar URL;
- ações de processo e logs;
- editor de configuração global e sobrescrita;
- diagnóstico com correções orientadas;
- menu na system tray;
- notificações apenas para eventos acionáveis;
- acessibilidade por teclado e tema claro/escuro;
- empacotamento e atualização segura.

Critério de aceite: as tarefas diárias podem ser feitas pela UI ou CLI com o mesmo resultado e sem divergência de estado.

## Fase 6 — operação completa

Objetivo: tornar a ferramenta confiável para uso diário por uma equipe.

- serviço Windows opcional, separado da UI;
- inicialização no login ou antes dele conforme perfil;
- API local autenticada entre CLI, UI e serviço;
- instalador assinado;
- atualização com checksum, rollback e canal estável/prévia;
- exportação/importação de configuração sem segredos;
- telemetria somente opt-in e sanitizada;
- diagnóstico exportável;
- documentação de falhas, recuperação e desinstalação;
- matriz automatizada de Windows, WSL e Ubuntu suportados.

Critério de aceite: instalação, atualização, recuperação e remoção são previsíveis em máquinas limpas e não alteram projetos do usuário.

## Definição de completo

O produto é considerado completo quando:

- PHP, estáticos e JS funcionam nos três modos de rota aplicáveis;
- padrões globais e sobrescritas por projeto são consistentes;
- CLI, Wails e serviço compartilham o mesmo domínio;
- configurações possuem validação e rollback;
- exposição de rede é restrita e visível;
- processos ociosos são encerrados conforme política;
- instalação e desinstalação não deixam firewall, serviço ou arquivos órfãos;
- testes e documentação cobrem os fluxos suportados.
