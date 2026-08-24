# Arquitetura

## Componentes

### Núcleo e CLI no Windows

Um executável Go concentra regras de negócio e oferece a CLI. Ele será responsável por:

- manter o registro de projetos;
- gerar configurações;
- executar comandos controlados no WSL por `wsl.exe`;
- validar e recarregar os dois Caddys;
- identificar o IP LAN;
- criar a regra de firewall durante a instalação;
- executar diagnósticos de ponta a ponta.

Operações administrativas devem ser pequenas e explícitas. A execução normal de `link`, `status` e `open` não deve pedir elevação.

### CLI no WSL planejada

O bootstrap instalará também um binário Linux `devlan` no WSL. Ele será um
cliente do núcleo controlador no Windows, não uma segunda implementação do
domínio. Sua função será interpretar caminhos e variáveis no namespace Linux,
identificar `WSL_DISTRO_NAME` e enviar comandos estruturados ao controlador.

O estado continuará autoritativo em `%LOCALAPPDATA%/DevLAN`. O cliente WSL não
manterá configuração concorrente em `$HOME`, e a comunicação deverá validar
versão, distribuição, operação e argumentos. O agente Linux futuro para
processos JavaScript poderá compartilhar o transporte, mas terá responsabilidade
separada da CLI interativa.

### Caddy no Windows

É a borda da rede. Escuta somente nos endereços e portas configurados, recebe
requisições da LAN e as encaminha para o WSL. Com SSL desligado, atende HTTP.
Com SSL ligado, mantém HTTP para redirect, termina TLS na porta 443 com a CA
interna do Caddy e encaminha ao mesmo upstream WSL.

Sua configuração deve mudar pouco. A lógica de PHP e de cada projeto permanece no WSL.

### Caddy no WSL

A API administrativa do Caddy no Windows usa `127.0.0.1:2019`, enquanto a do
Caddy no WSL usa `127.0.0.1:2020`. Endereços distintos evitam que o
encaminhamento de `localhost` do WSL entregue uma recarga ao processo errado.

Conhece os projetos, seus document roots, modos de atendimento e sockets PHP-FPM. A CLI gera fragmentos a partir do registro de projetos e só recarrega o Caddy após `caddy validate` ter sucesso.

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

### Runtime JavaScript futuro

Um agente no WSL será supervisor e reverse proxy para projetos JS. Ele deverá:

- detectar Bun, pnpm, Yarn ou npm pelo lockfile;
- ler apenas scripts permitidos do `package.json`;
- iniciar o servidor dev sob demanda;
- reservar uma porta estável por projeto;
- aguardar readiness antes de encaminhar;
- suportar WebSocket/HMR;
- encerrar processos após um idle timeout;
- nunca instalar dependências apenas porque ocorreu uma requisição HTTP.

## Fluxo de uma requisição PHP no MVP

```text
GET http://IP/financeiro/clientes
  1. Caddy/Windows recebe /financeiro/clientes
  2. encaminha ao Caddy/WSL com os headers externos
  3. Caddy/WSL seleciona financeiro e remove /financeiro
  4. php_fastcgi executa public/index.php
  5. Laravel responde ao cliente da LAN
```

Quando `tls_enabled = true`, a primeira etapa ocorre em
`https://IP/financeiro/clientes`; uma requisição HTTP recebe redirect 308. TLS
é global porque a negociação do certificado acontece antes de o Caddy conhecer
o subpath do projeto. Ainda assim, `secure`/`unsecure` recebem o nome ou caminho
do projeto e controlam os redirects e a URL anunciada para cada rota.

O Caddy armazena a CA e a chave privada no perfil do usuário Windows. Somente o
certificado raiz público pode ser copiado para clientes LAN. A chave privada e
o diretório de armazenamento do Caddy não devem ser compartilhados.

## Compatibilidade com Laravel em subpath

Servir Laravel em `/nome` exige cuidado porque redirects, assets, cookies e URLs absolutas podem assumir `/`.

No MVP, o adaptador Laravel deve:

- exigir `public/index.php` como document root;
- preservar headers `X-Forwarded-Host` e `X-Forwarded-Proto` do proxy;
- reescrever o header `Location` de redirects relativos para preservar o
  subpath externo;
- orientar `APP_URL=http://IP/nome`;
- verificar cache de configuração do Laravel;
- testar uma rota, redirect e asset no `doctor`;
- documentar quando `URL::forceRootUrl()` ou ajuste de proxy confiável for necessário.

Não se deve prometer compatibilidade universal com subpath. O roadmap inclui porta dedicada e hostname interno como alternativas por projeto.

## Estado e arquivos gerados

Proposta:

```text
%LOCALAPPDATA%/DevLAN/
  config.toml
  state.json
  generated/Caddyfile.windows
  generated/Caddyfile.wsl
  generated/php/php-8-5.conf
  generated/php/info/index.html
  logs/

/etc/devlan/
  generated/Caddyfile
  generated/php/
  snippets/
  backups/
```

- TOML guarda preferências editáveis.
- O estado JSON contém registro, overrides de projeto e versões PHP; SQLite fica reservado
  para quando processos, portas e migrações de schema entrarem no produto.
- Arquivos em `generated` não devem ser editados manualmente.
- Personalizações entram por snippets com pontos de extensão definidos.

No MVP, `generated/Caddyfile.windows` e `generated/Caddyfile.wsl` são arquivos
gerados pelo núcleo. O caminho Windows é convertido para `/mnt/<drive>/...`
quando o Caddy do WSL é validado ou recarregado, evitando concatenação de
comandos shell.

### Bootstrap da máquina

`scripts/install.ps1` é um adaptador de instalação, separado do domínio. Ele
baixa o código-fonte, instala Go e Caddy no Windows, chama
`scripts/install-wsl.sh` com argumentos separados para instalar PHP-FPM,
Composer, extensões Laravel e Caddy no WSL, compila `devlan.exe` e delega a
configuração final para o comando `devlan install`. A seleção de versões é
explícita; o script não instala dependências de projetos nem executa scripts
encontrados nos diretórios registrados.

Na Fase 1.1, o mesmo bootstrap também fará cross-compilation do cliente Linux e
o instalará como `/usr/local/bin/devlan` na distribuição selecionada.

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
