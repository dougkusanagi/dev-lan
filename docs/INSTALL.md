# Instalação

O DevLAN foi desenhado para Windows com WSL 2 e Ubuntu. O bootstrap completo
fica em `scripts/install.ps1`; ele baixa uma cópia do código, instala as
dependências do host, compila a CLI e prepara os dois Caddys.

## Instalação rápida

Abra o PowerShell como Administrador. Como este repositório ainda é privado,
use o token da sessão do GitHub para que `curl.exe` possa baixar o script e o
arquivo-fonte:

```powershell
$env:GH_TOKEN = gh auth token; $p = Join-Path $env:TEMP 'devlan-install.ps1'; curl.exe -fsSL -H "Authorization: Bearer $env:GH_TOKEN" https://raw.githubusercontent.com/dougkusanagi/dev-lan/master/scripts/install.ps1 -o $p; powershell.exe -NoProfile -ExecutionPolicy Bypass -File $p
```

Se o repositório for público, o header de autorização pode ser removido:

```powershell
$p = Join-Path $env:TEMP 'devlan-install.ps1'; curl.exe -fsSL https://raw.githubusercontent.com/dougkusanagi/dev-lan/master/scripts/install.ps1 -o $p; powershell.exe -NoProfile -ExecutionPolicy Bypass -File $p
```

Ao executar o script a partir de um clone local, não é necessário baixar o
código novamente:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\install.ps1 -SourceDir .
```

Depois da instalação, abra um terminal novo e verifique:

```powershell
devlan doctor
devlan status
```

## O que o bootstrap instala

- WSL/Ubuntu quando não existe uma distribuição instalada; nesse caso o
  Windows precisa ser reiniciado e o instalador deve ser executado novamente.
- Go estável para Windows amd64, baixado da lista oficial do Go quando o
  comando ainda não existe. O toolchain local fica em
  `%LOCALAPPDATA%\DevLAN\toolchains\go`.
- Caddy no Windows, primeiro pelo pacote `CaddyServer.Caddy` do `winget` e,
  como fallback, pelo release oficial com verificação SHA-256.
- PHP-FPM e extensões comuns do Laravel no WSL, além de Composer.
- Caddy no WSL pelo repositório oficial Debian/Ubuntu.
- Pool PHP-FPM compartilhado com `pm=ondemand`, dez workers máximos,
  `pm.process_idle_timeout=10s` e `pm.max_requests=500`.
- `devlan.exe` em `%LOCALAPPDATA%\DevLAN\bin`, PATH do usuário e a regra de
  firewall privada do DevLAN.

O script não executa `composer install` nos projetos e não instala bancos,
Node ou dependências de aplicação. Esses passos continuam explícitos e fora do
MVP.

## Versão do PHP

PHP não possui uma categoria oficial chamada “LTS”: cada branch recebe dois
anos de suporte ativo e dois anos de correções críticas de segurança. O
bootstrap usa PHP 8.5 por padrão por ser a branch ativa mais recente do MVP;
8.3 e 8.4 também podem ser escolhidos:

```powershell
.\scripts\install.ps1 -SourceDir . -PhpVersion 8.4
```

Quando a distribuição não oferece a branch escolhida, o script tenta o PPA
`ondrej/php` em Ubuntu e, se ainda assim não houver pacote, usa a versão
`php-fpm` padrão da distribuição. `devlan doctor` mostra a versão encontrada.

## Opções úteis

```text
-Distribution Ubuntu-24.04  escolhe a distribuição WSL
-PhpVersion 8.3|8.4|8.5     escolhe a branch PHP (padrão: 8.5)
-InstallRoot PATH            altera o diretório gerenciado
-SkipWSL                     não instala dependências no WSL
-SkipCaddy                   não instala Caddy
-NoFirewall                  não cria a regra de firewall nesta execução
-NoPath                      não persiste alterações no PATH do usuário
-Ref TAG_OU_COMMIT           fixa a versão do código baixado
```

Para uma instalação reproduzível, fixe `-Ref` em uma tag ou commit e revise o
script antes de executá-lo. O instalador exige elevação porque WSL, porta 80 e
firewall são componentes do sistema.

Referências oficiais: [instalação do WSL](https://learn.microsoft.com/en-us/windows/wsl/install),
[downloads do Go](https://go.dev/dl/), [versões suportadas do PHP](https://www.php.net/supported-versions.php)
e [instalação do Caddy](https://caddyserver.com/docs/install).
