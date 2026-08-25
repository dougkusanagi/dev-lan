# Instalação

O DevLAN foi desenhado para Windows com WSL 2 e Ubuntu. O bootstrap completo
fica em `scripts/install.ps1`; ele baixa uma cópia do código, instala as
dependências do host, compila a CLI e prepara os dois Caddys.

## Instalação rápida

Abra o PowerShell como Administrador e execute:

```powershell
curl.exe -fsSL https://raw.githubusercontent.com/dougkusanagi/dev-lan/master/scripts/install.ps1 -o "$env:TEMP\devlan-install.ps1"; powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$env:TEMP\devlan-install.ps1"
```

O instalador interrompe a execução com uma mensagem clara quando o terminal não
está elevado. A conta usada continua sendo a conta atual; a elevação serve para
configurar os componentes do sistema e a regra de firewall.

As etapas de provisionamento dentro do WSL são executadas diretamente como
`root` pelo `wsl.exe`, sem solicitar a senha do `sudo`. Esse privilégio é usado
somente durante a instalação de pacotes, configuração do PHP-FPM e do Caddy; os
comandos cotidianos do DevLAN continuam sendo executados com o usuário normal.

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

O bootstrap pode ser executado novamente com segurança. Go e Caddy no Windows
são reutilizados quando já existem; no WSL, PHP-FPM da versão escolhida, suas
extensões, Composer e Caddy são verificados antes de executar `apt update` ou
`apt install`. A CLI é recompilada e os arquivos gerenciados são sincronizados
para aplicar atualizações do DevLAN.

Depois da instalação, HTTPS é opcional:

```powershell
devlan secure NAME|PATH
```

Execute esse comando em PowerShell como Administrador na primeira ativação para
permitir a porta 443 no firewall e confiar na CA interna no Windows. O uso
normal da CLI continua sem elevação. Outros dispositivos precisam importar o
certificado raiz `%APPDATA%\Caddy\pki\authorities\local\root.crt`; consulte
[CLI e configuração](CLI-AND-CONFIG.md#https-na-lan).

O script não executa `composer install` nos projetos e não instala bancos,
Node ou dependências de aplicação. Esses passos continuam explícitos e fora do
MVP.

## Operação após a instalação

Para manter o controlador disponível sem a UI, instale o serviço opcional em
um PowerShell elevado:

```powershell
devlan service install
devlan service start
devlan service status
```

O serviço usa a mesma configuração da CLI e escuta somente a API autenticada
em loopback. Para uma inicialização de login sem SCM, use
`devlan startup enable gui`; detalhes de recuperação, exportação e diagnóstico
estão em [Operação, recuperação e suporte](OPERATIONS.md).

## Fase 2: mais de uma versão PHP

O bootstrap continua instalando a versão inicial escolhida por `-PhpVersion`.
Depois, as demais branches podem ser adicionadas sem reinstalar o DevLAN:

```powershell
devlan php install 8.3
devlan php install 8.5
devlan php list
devlan php use default 8.5
```

Cada instalação registra a versão, instala Composer e as extensões solicitadas,
gera um mestre PHP-FPM com `pm=ondemand` e mantém workers ociosos somente até
`pm.process_idle_timeout`. Para ajustar a política:

```powershell
devlan php pool default --max-children 10 --idle-timeout 10s --max-requests 500
devlan php pool 8.3 --max-children 20
devlan php pool meu-projeto isolated
```

`devlan php remove VERSION` remove apenas versões que não estão selecionadas
explicitamente por um projeto. Diretórios dos projetos e suas dependências não
são removidos.

O binário `devlan` nativo para uso direto dentro do WSL está planejado para a
Fase 1.1 e ainda não é instalado pelo bootstrap atual. Ele será um cliente da
CLI/controlador Windows, preservando uma única fonte de estado.

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
-WindowsPort PORT            fixa a porta HTTP do Windows (padrão: automática)
-InstallRoot PATH            altera o diretório gerenciado
-SkipWSL                     não instala dependências no WSL
-SkipCaddy                   não instala Caddy
-NoFirewall                  não cria a regra de firewall nesta execução
-NoPath                      não persiste alterações no PATH do usuário
-Ref TAG_OU_COMMIT           fixa a versão do código baixado
```

O bootstrap prefere a porta `80`. Se ela estiver ocupada por Docker, Podman,
WSL ou outro serviço, escolhe automaticamente `8080`, `8081` ou `8888` e salva
essa escolha na configuração. Use `-WindowsPort` para exigir uma porta específica.

Para uma instalação reproduzível, fixe `-Ref` em uma tag ou commit e revise o
script antes de executá-lo. O instalador exige elevação porque WSL, porta 80 e
firewall são componentes do sistema.

Referências oficiais: [instalação do WSL](https://learn.microsoft.com/en-us/windows/wsl/install),
[downloads do Go](https://go.dev/dl/), [versões suportadas do PHP](https://www.php.net/supported-versions.php)
e [instalação do Caddy](https://caddyserver.com/docs/install).
