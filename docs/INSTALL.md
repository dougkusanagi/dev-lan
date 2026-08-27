# Instalação

O DevLAN foi desenhado para Windows com WSL 2 e Ubuntu. O bootstrap completo
fica em `scripts/install.ps1`; ele baixa uma cópia do código, instala as
dependências do host, compila a CLI e prepara um único Caddy no WSL.

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

O bootstrap existe para colocar a CLI e os pré-requisitos na máquina. Depois
disso, `devlan install` é o comando idempotente para criar ou reconciliar os
arquivos gerenciados, as configurações de Caddy/PHP-FPM e a regra de firewall. Execute-o em um
PowerShell elevado quando a operação envolver firewall ou certificados:

```powershell
devlan install
```

## O que o bootstrap instala

- WSL/Ubuntu quando não existe uma distribuição instalada; nesse caso o
  Windows precisa ser reiniciado e o instalador deve ser executado novamente.
- Go estável para Windows amd64, baixado da lista oficial do Go quando o
  comando ainda não existe. O toolchain local fica em
  `%LOCALAPPDATA%\DevLAN\toolchains\go`.
- PHP-FPM e extensões comuns do Laravel no WSL, além de Composer.
- systemd e Caddy no WSL pelo repositório oficial Debian/Ubuntu.
- `.wslconfig` preparado de forma transacional para `networkingMode=mirrored`,
  `firewall=true`, `dnsTunneling=true` e `autoProxy=true`.
- Pool PHP-FPM compartilhado com `pm=ondemand`, dez workers máximos,
  `pm.process_idle_timeout=10s` e `pm.max_requests=500`.
- `devlan.exe` em `%LOCALAPPDATA%\DevLAN\bin`, PATH do usuário e as regras
  mínimas do Windows Firewall/Hyper-V Firewall.

O executável instalado usa o subsistema de console do Windows: `devlan`,
`devlan -h` e mensagens de erro sempre aparecem no PowerShell. A interface
Wails é iniciada explicitamente por `devlan gui`.

O bootstrap pode ser executado novamente com segurança. No WSL, PHP-FPM da
versão escolhida, suas extensões, Composer, systemd e Caddy são verificados
antes de executar `apt update` ou `apt install`. A CLI é recompilada e o
Caddyfile único é publicado pelo controlador.

O instalador não executa `wsl --shutdown` por padrão. Depois de salvar o
trabalho nas distribuições WSL, aplique o modo espelhado explicitamente:

```powershell
devlan topology check
devlan topology migrate --yes
```

O segundo comando encerra todas as distribuições WSL, não apenas a escolhida
no DevLAN. Para um bootstrap automatizado que já recebeu essa confirmação,
use `-ConfirmWSLShutdown` no script.

Depois da instalação, HTTPS é opcional:

```powershell
devlan secure NAME|PATH
```

Execute esse comando em PowerShell como Administrador na primeira ativação para
permitir a porta 443 no Windows Firewall/Hyper-V Firewall e instalar no Windows
o certificado raiz público emitido pelo Caddy WSL. O uso normal da CLI continua
sem elevação. Outros dispositivos precisam importar o certificado raiz
exportado por `devlan ca export`; a chave privada nunca é exportada. Consulte
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

O bootstrap também compila e instala `/usr/local/bin/devlan` como cliente Linux
para uso direto dentro do WSL. Ele encaminha as operações essenciais ao
controlador Windows pela API local autenticada e preserva uma única fonte de
estado em `%LOCALAPPDATA%/DevLAN`. O serviço Windows (ou a UI) precisa estar
em execução para receber os comandos.

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
-WindowsPort PORT            legado; a borda M8 usa 80/443 no WSL
-InstallRoot PATH            altera o diretório gerenciado
-SkipWSL                     não instala dependências no WSL
-SkipCaddy                   não instala Caddy
-NoFirewall                  não cria a regra de firewall nesta execução
-NoPath                      não persiste alterações no PATH do usuário
-ConfirmWSLShutdown          confirma o shutdown de todas as distros WSL
-Ref TAG_OU_COMMIT           fixa a versão do código baixado
```

O Caddy WSL usa diretamente `80` e `443` e as portas atribuídas no pool de
rotas (por padrão `8080–8179`). Se houver conflito, `devlan topology check`
mostra cada porta ocupada antes da migração; `ui_port` continua restrita ao
loopback e nunca entra no firewall LAN.

Para uma instalação reproduzível, fixe `-Ref` em uma tag ou commit e revise o
script antes de executá-lo. O instalador exige elevação porque WSL, porta 80 e
firewall são componentes do sistema.

Referências oficiais: [instalação do WSL](https://learn.microsoft.com/en-us/windows/wsl/install),
[downloads do Go](https://go.dev/dl/), [versões suportadas do PHP](https://www.php.net/supported-versions.php)
e [instalação do Caddy](https://caddyserver.com/docs/install).
