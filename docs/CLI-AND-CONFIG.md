# CLI e configuração

## Registro de projetos

### Link explícito

```powershell
devlan link financeiro /home/silver/dev/financeiro
devlan unlink financeiro
devlan links
```

`links` exibe as colunas `PROJETO`, `MODO`, `ORIGEM`, `SSL`, `URL` e
`CAMINHO`. Como todos os projetos compartilham a mesma borda IP/porta, `SSL`
reflete uma política global e aparece como `on` ou `off` em todas as linhas.

Uma CLI Linux está planejada para a Fase 1.1. Depois de instalada pelo
bootstrap, permitirá executar no WSL:

```bash
devlan park ~/Sites
```

Ela encaminhará a operação ao controlador Windows; não manterá uma configuração
separada no WSL. Até essa fase ser implementada, caminhos WSL devem ser passados
à CLI Windows como caminhos Linux absolutos e com capitalização exata.

`link` associa um nome estável a um diretório específico e tem prioridade sobre projetos descobertos por `park`.

### Diretórios estacionados

```powershell
devlan park /home/silver/dev
devlan unpark /home/silver/dev
devlan parked
```

Cada filho direto contendo um projeto reconhecido pode ser publicado com o nome do diretório. A descoberta não deve executar scripts.

## Modos de atendimento

Valores planejados:

- `auto`: detecta PHP, servidor JS ou saída estática;
- `php`: Caddy e PHP-FPM;
- `dev`: script de desenvolvimento do projeto JS;
- `static`: pasta já compilada, como `dist`.

O modo pode ser definido globalmente:

```powershell
devlan mode default auto
```

E sobrescrito por projeto:

```powershell
devlan mode painel dev
devlan mode site static
devlan mode financeiro php
```

Para voltar a herdar o padrão global:

```powershell
devlan mode painel inherit
```

Regra de resolução:

```text
modo explícito do projeto
  > configuração da entrada park
  > padrão global
```

Na Fase 1, `php` usa o socket legado configurado em `php_fpm_socket`. Na Fase
2, versões registradas ganham sockets e pools próprios sem invalidar esse
estado antigo.

## PHP completo

Instale e mantenha as versões disponíveis no WSL:

```powershell
devlan php install 8.3
devlan php install 8.5 --extensions mbstring,xml,curl
devlan php list
devlan php remove 8.3
```

O primeiro `php install` de uma configuração vazia torna-se a versão global.
Depois, a preferência pode ser alterada sem mexer nos projetos:

```powershell
devlan php use default 8.5
devlan php use financeiro 8.3
devlan php use financeiro inherit
```

A resolução da versão é `sobrescrita do projeto > versão global`. Quando há
versões registradas, o DevLAN rejeita uma referência a uma versão que não foi
instalada. Projetos antigos sem `php_versions` continuam usando
`/run/php/php-fpm.sock`.

Extensões são uma lista por versão:

```powershell
devlan php extensions 8.5
devlan php extensions 8.5 mbstring xml intl
```

Pools usam `ondemand` por padrão. Os limites podem ser ajustados globalmente
ou por versão:

```powershell
devlan php pool default --max-children 10 --idle-timeout 10s --max-requests 500
devlan php pool 8.5 --max-children 20 --idle-timeout 15s
devlan php pool financeiro isolated
devlan php pool financeiro shared
```

Um pool compartilhado usa `/run/devlan/php/VERSAO/shared.sock`; um pool isolado
usa `/run/devlan/php/VERSAO/NOME.sock`. Os arquivos gerados ficam em
`generated/php` e não devem ser editados manualmente.

Presets podem ser detectados por markers sem executar scripts ou definidos
explicitamente:

```powershell
devlan php preset financeiro laravel
devlan php preset portal symfony
devlan php preset site generic
```

Composer pode usar o binário do sistema ou ser executado pelo PHP da versão
selecionada. O ambiente global ou por projeto é configurável:

```powershell
devlan composer config default per-version
devlan composer config financeiro system
devlan composer financeiro --environment per-version -- install --no-interaction
```

`php info [NAME]` imprime uma página HTML sanitizada. Ela mostra apenas
projeto, preset, versão, pool, socket e extensões; não exibe `$_SERVER`,
variáveis de ambiente, headers, credenciais ou conteúdo da aplicação.

## Configuração por projeto

Exemplos futuros:

```powershell
devlan php use financeiro 8.3
devlan config financeiro route.mode path
devlan config financeiro route.path financeiro
devlan config painel js.idle-timeout 15m
devlan config painel static.dir dist
```

Um comando deve mostrar valor efetivo e origem:

```powershell
devlan config painel --resolved
```

```text
mode                 dev       project
js.idle-timeout      15m       global
route.mode           path      default
```

## Comandos do MVP

```text
devlan install [--no-firewall]  prepara Caddy, PHP-FPM e firewall
devlan uninstall                remove componentes gerenciados, preserva projetos
devlan link NAME PATH           registra um projeto Laravel
devlan unlink NAME              remove o registro e a rota
devlan park PATH                registra uma pasta de projetos
devlan unpark PATH              remove a pasta estacionada
devlan status                   mostra componentes, projetos e URLs
devlan open [NAME]              abre projeto ou dashboard textual
devlan reload                   valida e aplica configurações
devlan secure NAME|PATH         ativa HTTPS para um projeto
devlan unsecure NAME|PATH       desativa HTTPS para um projeto
devlan doctor [NAME]            diagnóstico completo ou por projeto
devlan logs [COMPONENT]         exibe logs relevantes
devlan mode default php         define modo global
devlan mode NAME php|inherit    sobrescreve ou restaura herança
devlan php list                 lista versões PHP e extensões
devlan php install VERSION      instala PHP-FPM e Composer
devlan php use NAME VERSION     seleciona PHP por projeto
devlan php pool ...             configura pools e timeout
devlan php preset NAME PRESET   escolhe preset PHP
devlan php info [NAME]          página de informações sanitizada
devlan composer VERSION|NAME    executa Composer com PHP selecionado
```

## HTTPS na LAN

```powershell
devlan secure NAME|PATH
devlan links
devlan unsecure NAME|PATH
```

`secure` habilita a porta 443 no Caddy do Windows, emite um certificado para o
IP LAN usando a CA interna do Caddy e mantém a porta HTTP apenas para redirect.
O comando tenta atualizar a regra de firewall e confiar na CA no Windows. Se o
PowerShell atual não for administrativo, ele mantém a configuração aplicada e
avisa quais etapas exigem executar `devlan secure` novamente como Administrador.

Certificados de uma CA interna não são confiados automaticamente por outros
computadores e celulares. Em cada cliente da LAN, importe como autoridade raiz
confiável o arquivo `%APPDATA%\Caddy\pki\authorities\local\root.crt` gerado na
máquina do DevLAN. Distribua esse arquivo apenas por um canal confiável e nunca
distribua a chave privada ao lado dele.

`unsecure` remove o listener HTTPS e o redirect, preservando projetos, parks e
dados. A configuração gerenciada registra `tls_enabled` e `https_port`; a porta
HTTPS padrão é 443.

Para instalar Go, WSL/Ubuntu, Caddy, PHP-FPM, extensões Laravel e Composer em
uma máquina limpa, use `scripts/install.ps1` conforme [o guia de instalação](INSTALL.md).
O comando `devlan install` é a etapa idempotente do núcleo: cria/atualiza os
arquivos gerenciados e a regra de firewall, mas não instala dependências de um
projeto específico.

O binário aceita `--data-dir DIR` antes do comando. Sem essa opção, usa
`%LOCALAPPDATA%/DevLAN` no Windows ou `~/.devlan` em outros sistemas. O
diretório contém `config.toml`, `state.json`, os Caddyfiles gerados e logs.

`park PATH` registra a pasta, mas não copia projetos para o estado. Em cada
geração, filhos diretos são examinados sem executar scripts; somente os que
contêm `artisan` e `public/index.php` tornam-se rotas. Um `link` explícito tem
prioridade se houver colisão de nome. `link`, `unlink`, `park`, `unpark` e as
mudanças de modo aplicam a configuração automaticamente: a CLI recarrega um
Caddy em execução ou inicia o Caddy Windows quando ele ainda não estiver ativo.

`reload` cria arquivos temporários, valida os Caddyfiles disponíveis, substitui
os arquivos gerados e só então tenta o reload. Se o reload falhar, restaura o
par anterior. Quando Caddy ou PHP-FPM não estão instalados, a configuração
continua sendo gerada e `doctor` informa a dependência ausente.

## Comandos posteriores

```text
devlan php install 8.5
devlan php use NAME 8.5
devlan deps install NAME
devlan build NAME
devlan start|stop|restart NAME
devlan expose NAME --mode path|port|host
devlan mode NAME auto|php|dev|static
devlan ui
```

## Detecção JavaScript futura

Prioridade inicial de lockfiles:

```text
bun.lock / bun.lockb → Bun
pnpm-lock.yaml       → pnpm
yarn.lock            → Yarn
package-lock.json    → npm
```

Se mais de um lockfile existir, `doctor` deve marcar ambiguidade em vez de escolher silenciosamente. `packageManager` no `package.json`, quando válido, pode resolver a ambiguidade.

O comando padrão vem de `scripts.dev`, mas pode ser sobrescrito explicitamente. Apenas projetos confiáveis e registrados podem iniciar processos.
