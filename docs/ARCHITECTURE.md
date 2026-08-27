# Arquitetura

## Componentes

### Núcleo e CLI no Windows

Um executável Go concentra regras de negócio e oferece a CLI. Ele será responsável por:

- manter o registro de projetos;
- gerar configurações;
- executar comandos controlados no WSL por `wsl.exe`;
- validar e recarregar o Caddy único no WSL;
- identificar o IP LAN;
- criar a regra de firewall durante a instalação;
- executar diagnósticos de ponta a ponta.

Operações administrativas devem ser pequenas e explícitas. A execução normal de `link`, `status` e `open` não deve pedir elevação.

### Fronteira WSL medida

O núcleo usa `wsl.exe` como transporte do execution plane e agrupa discovery,
status, sondagens de binários/sockets e ACLs com argumentos posicionais. O
dashboard browser-first consulta o read model agregado
`GET /api/v1/overview`, evitando materializar projetos, status e PHP em três
polls independentes. O runner mantém contagem e duração agregadas sem guardar
comandos; budgets e a decisão de não criar agente Linux persistente estão em
[Integração WSL — Marco 7](WSL-EXECUTION-PLANE.md).

### CLI no WSL

O bootstrap instala também um binário Linux `devlan` no WSL. Ele é um cliente
fino do núcleo controlador no Windows, não uma segunda implementação do
domínio. Sua função é interpretar caminhos no namespace Linux e enviar
comandos estruturados pelo endpoint loopback autenticado `/v1/command`.

O estado continua autoritativo em `%LOCALAPPDATA%/DevLAN`. O cliente WSL não
mantém configuração concorrente em `$HOME`; a distribuição usa um arquivo
somente de configuração que aponta para o diretório montado do Windows. A API
valida versão, token, origem loopback, operação e argumentos. O agente Linux
para processos JavaScript continua sendo uma responsabilidade separada.

### Caddy único no WSL

Windows 11 22H2+, WSL 2, `networkingMode=mirrored` e systemd são pré-requisitos
do execution plane. A instância Caddy gerenciada por systemd no WSL é a única
borda: escuta 80/443 e as portas LAN atribuídas pelo alocador persistente. Ela
serve `https://nome.localhost/`, `http(s)://IP:porta/` e
`https://devlan.localhost/` diretamente no WSL.

Os sites `.localhost` ficam limitados a `127.0.0.1`/`::1`. O dashboard é o
único reverse proxy para o Windows, em `127.0.0.1:<ui_port>`; PHP-FPM, static,
Vite/SSR, WebSocket e assets usam os caminhos e sockets do WSL diretamente. A
API administrativa do Caddy permanece em `127.0.0.1:2020` e nunca é publicada
na LAN.

Cada projeto recebe uma porta LAN do pool persistido por caminho; a ordem de
descoberta não altera uma atribuição existente. O Windows Firewall e o Hyper-V
Firewall são reconciliados em conjunto para `Private`/`LocalSubnet`, enquanto
`ui_port` continua loopback-only. A aplicação segue
`plan → validate → stage → commit → reload → healthcheck`, com systemd, arquivo
vivo e backup usados para detectar e recuperar uma falha parcial.

### Compatibilidade e migração

`devlan topology check` consulta build do Windows, versão/WSL2, modo espelhado
efetivo, systemd, loopback, alcance LAN e conflitos em 80/443/pool. O editor
transacional do `.wslconfig` altera somente as chaves gerenciadas em `[wsl2]`,
preserva comentários e seções desconhecidas e mantém backup.

`devlan topology repair` reconcilia a configuração, firewall e Caddy sem
interromper distribuições. `devlan topology migrate --yes` é o único fluxo que
executa `wsl --shutdown`: valida e sobe o Caddy novo, verifica health, para a
topologia antiga, reinicia/valida o novo serviço e só então remove artefatos
legados. Qualquer falha posterior restaura os backups disponíveis.

### PHP-FPM

Projetos registrados podem apontar para versões PHP diferentes. Cada versão
gerenciada tem um mestre PHP-FPM independente e, por padrão, um pool
compartilhado:

```ini
pm = ondemand
pm.max_children = 10
pm.process_idle_timeout = 10s
pm.max_requests = 500
```

O processo mestre permanece ativo, mas workers ociosos são encerrados por
`pm.process_idle_timeout`. Um projeto pode receber um pool isolado, com socket
e logs próprios, sem compartilhar workers com os demais projetos da mesma
versão. A resolução da versão é `projeto > global`; a configuração de pool é
`projeto > versão > global`.

Os sockets gerenciados seguem:

```text
/run/devlan/php/8.3/shared.sock
/run/devlan/php/8.3/financeiro.sock
/run/devlan/php/8.5/shared.sock
```

As configurações de mestres ficam em `generated/php/php-VERSAO.conf`. O
gerenciador WSL cria os diretórios de runtime, inicia cada mestre com
argumentos estruturados e recarrega um mestre existente pelo PID, tornando
`reload` idempotente.

Composer é executado como `composer` no ambiente `system` ou como
`phpVERSAO composer` no ambiente `per-version`. Os argumentos do projeto são
encaminhados como argumentos separados; nenhum script do projeto é executado
durante detecção ou instalação.

### Presets e informações PHP

O detector reconhece Laravel por `artisan` + `public/index.php`, Symfony por
`bin/console` + `public/index.php` e o preset genérico por `public/index.php`
ou `index.php` na raiz. A detecção usa somente markers e não executa Composer,
CLI do framework ou código da aplicação.

`php info` é renderizado por um template HTML com escape contextual e uma
allowlist de campos. A página não é um `phpinfo()` e não inclui ambiente,
headers, valores de request ou segredos.

### Runtime JavaScript

O supervisor mantém um gateway estável para cada projeto JS e atua como reverse
proxy para o processo backend. Ele:

- detectar Bun, pnpm, Yarn ou npm pelo lockfile;
- ler apenas scripts permitidos do `package.json`;
- iniciar o servidor dev sob demanda após a primeira requisição;
- reservar uma porta estável por projeto;
- aguardar readiness antes de encaminhar;
- suportar WebSocket/HMR;
- encerrar processos após um idle timeout medido pela atividade do gateway;
- nunca instalar dependências apenas porque ocorreu uma requisição HTTP.

## Fluxo de uma requisição PHP

```text
GET http://IP:8080/clientes
  1. Caddy WSL recebe diretamente a porta 8080
  2. seleciona o site do projeto pela porta atribuída
  3. php_fastcgi executa public/index.php
  4. Laravel responde ao cliente da LAN
```

No acesso local, o Caddy usa sempre `https://financeiro.localhost/clientes`.
Na LAN, cada projeto possui uma porta dedicada e serve a aplicação na raiz:
`http(s)://IP:8080/clientes`. Quando o TLS global e a preferência segura do
projeto estão ativos, a origem LAN usa HTTPS; não há seleção de modo, subpath
externo ou hostname LAN por projeto.

O Caddy armazena a CA e a chave privada somente no WSL. O DevLAN exporta e
valida apenas o certificado raiz público para instalar no trust store do
Windows ou distribuir manualmente a clientes LAN; a chave privada nunca cruza
essa fronteira.

## Compatibilidade com Laravel na raiz

Servir Laravel na raiz preserva a semântica esperada por redirects, assets,
cookies e URLs absolutas.

O adaptador Laravel deve:

- exigir `public/index.php` como document root;
- preservar headers `X-Forwarded-Host` e `X-Forwarded-Proto` do proxy;
- orientar `APP_URL` para a origem escolhida (`https://nome.localhost/` ou
  `http(s)://IP:porta/`);
- verificar cache de configuração do Laravel;
- testar uma rota, redirect e asset no `doctor`;
- documentar quando `URL::forceRootUrl()` ou ajuste de proxy confiável for necessário.

## Estado e arquivos gerados

Proposta:

```text
%LOCALAPPDATA%/DevLAN/
  config.toml
  state.json
  wsl-distribution
  generated/Caddyfile
  generated/Caddyfile.previous
  generated/php/php-8-5.conf
  generated/php/info/index.html
  logs/

/etc/caddy/Caddyfile       (cópia viva publicada atomicamente)
/etc/devlan/
  generated/php/
  backups/
```

- TOML guarda preferências editáveis.
- O estado JSON contém registro, overrides de projeto e versões PHP; SQLite fica reservado
  para quando processos, portas e migrações de schema entrarem no produto.
- Arquivos em `generated` não devem ser editados manualmente.
- Personalizações entram por snippets com pontos de extensão definidos.

`generated/Caddyfile` é o único artefato de borda gerado pelo núcleo. O caminho
Windows é convertido para `/mnt/<drive>/...` quando o Caddy do WSL é validado
ou publicado em `/etc/caddy/Caddyfile`, evitando concatenação de comandos shell.

### Bootstrap da máquina

`scripts/install.ps1` é um adaptador de instalação, separado do domínio. Ele
baixa o código-fonte, instala Go no Windows, chama
`scripts/install-wsl.sh` com argumentos separados para instalar PHP-FPM,
Composer, extensões Laravel, systemd e Caddy no WSL, compila `devlan.exe` e o cliente
Linux do WSL e delega a configuração final para o comando `devlan install`. A
seleção de versões é
explícita; o script não instala dependências de projetos nem executa scripts
encontrados nos diretórios registrados.

O cliente Linux é instalado como `/usr/local/bin/devlan` na distribuição
selecionada e usa o diretório montado do estado Windows.

## Aplicação segura de configuração

Toda mudança segue:

```text
registrar intenção
  → gerar temporário
  → validar
  → substituir atomicamente
  → reload
  → health check
  → confirmar ou rollback
```

Entradas do usuário nunca devem ser concatenadas em comandos shell. Caminhos e argumentos são enviados como argumentos separados e validados contra o registro de projetos.

Para projetos no filesystem Linux, o núcleo aplica ACLs restritas ao projeto:
`caddy` recebe leitura e travessia; `www-data` recebe leitura/escrita para que
PHP-FPM possa servir e gravar arquivos de runtime. ACLs padrão são aplicadas a
subdiretórios para arquivos futuros. O DevLAN não altera permissões globais do
home do usuário.

## Operação da Fase 6

O serviço Windows opcional é um cliente de controle, não uma segunda
implementação do domínio. O SCM inicia o mesmo executável com `service run`; o
processo cria uma API HTTP em `127.0.0.1`, carrega o mesmo `config.toml`/
`state.json`, aplica a configuração e encerra de forma controlada em
`SERVICE_STOP` ou `SERVICE_SHUTDOWN`. A UI Wails e a CLI podem continuar usando
o `app.App` diretamente ou o transporte versionado da API.

O token da API é criado com aleatoriedade criptográfica e armazenado fora do
estado exportável. O endpoint é escolhido em porta loopback efêmera e fica em
um arquivo de estado de processo; nenhuma porta de controle é aberta na LAN.

Exportações passam por uma cópia profunda sanitizada antes da serialização.
Diagnósticos recebem apenas uma allowlist explícita de arquivos gerenciados e
aplicam redação específica a blocos `basicauth`. Telemetria tem consentimento
persistido separado, fila local e envio manual; não participa do fluxo normal
de reload.
