# Operação, recuperação e suporte

Este documento descreve os recursos operacionais disponíveis no binário. O
estado autoritativo continua em `%LOCALAPPDATA%/DevLAN` (ou no
valor de `DEVLAN_HOME`); os diretórios dos projetos nunca fazem parte de um
backup ou diagnóstico automático.

## Agente do usuário e serviço Windows

Para instalações com WSL por usuário, o caminho normal é iniciar a API na
sessão interativa:

```powershell
devlan startup enable service
devlan startup status
```

Esse comando grava uma entrada em `HKCU` e mantém a API disponível sem a UI,
com a mesma identidade que possui a distro WSL.

O serviço SCM é uma opção avançada para ambientes em que a conta do sistema
possui acesso ao WSL:

```powershell
devlan service install --system
devlan service start
devlan service status
devlan service stop
devlan service remove
```

`service install --system` registra `DevLAN` no Service Control Manager com
início automático no boot e exige um terminal elevado. O DevLAN executa um
preflight de acesso ao WSL antes de abrir a API; se a conta não enxergar a
distro configurada, o serviço falha claramente e não assume a porta 3210.

Para uma instalação legada que criou o serviço como `LocalSystem`, pare-o e
deixe o agente do usuário assumir a API:

```powershell
# PowerShell como Administrador
sc.exe stop DevLAN
sc.exe config DevLAN start= demand

# PowerShell normal, como o usuário que possui a distro WSL
devlan startup enable service
```

Para controlar o início no login do usuário:

```powershell
devlan startup enable gui
devlan startup enable service
devlan startup status
devlan startup disable
```

O modo `service` inicia o servidor de API em uma sessão interativa. O modo
`gui` abre o shell Wails explicitamente. O comando sem modo usa `service`.
O valor é gravado somente em `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`.

Locks órfãos são recuperados pelo PID quando o processo proprietário morreu.
Se a recuperação não for possível, o fallback conservador continua sendo a
expiração por idade; nenhum projeto dentro de um park é removido durante essa
operação.

## API local autenticada

O serviço e clientes locais compartilham uma API HTTP limitada a
`127.0.0.1`. Ao iniciar, o DevLAN cria:

```text
%LOCALAPPDATA%/DevLAN/api.token
%LOCALAPPDATA%/DevLAN/api.endpoint.json
```

O token é aleatório, persistente entre reinícios e protegido com permissões de
usuário. A API recusa requisições sem `Authorization: Bearer TOKEN`, de outras
interfaces ou para rotas desconhecidas. A versão atual expõe:

```text
GET  /v1/health
GET  /v1/overview
GET  /v1/status
GET  /v1/topology
GET  /v1/projects
GET  /v1/config
POST /v1/reload
POST /v1/command   (cliente Linux WSL; allowlist de operações)
```

O dashboard browser-first usa `GET /v1/overview` (também disponível em
`/api/v1/overview`) para receber projetos, status e versões PHP na mesma
fotografia. Isso evita três polls independentes e mantém a descoberta/status
WSL agrupados.

Para verificar o processo em execução:

```powershell
devlan api status
```

`api serve` é útil para desenvolvimento e para ambientes sem o serviço:

```powershell
devlan api serve
```

CLI e UI podem chamar o núcleo Go diretamente; o cliente Linux usa `/v1/command`
para encaminhar `link`, `unlink`, `park`, `unpark`, `links`, `status`, `topology`,
`reload`, `doctor` e `open`. Todas as formas usam o mesmo modelo e validação,
sem expor a porta de controle na LAN.

## Caddy único, mirrored networking e migração

O estado live da topologia pode ser consultado sem interromper o WSL:

```powershell
devlan topology status
devlan topology check
devlan topology repair
```

O diagnóstico exige Windows 11 22H2+, WSL 2, `networkingMode=mirrored`,
systemd, loopback bidirecional, alcance LAN e disponibilidade de 80/443 e do
pool. A política conjunta usa Windows Firewall e Hyper-V Firewall em
`Private`/`LocalSubnet`, com default inbound `Block`; `ui_port` permanece
loopback-only.

Para migrar uma instalação antiga, faça backup do trabalho em todas as
distribuições e execute:

```powershell
devlan topology migrate --yes
```

O fluxo faz backup dos Caddyfiles, valida e inicia o Caddy WSL, verifica o
healthcheck, interrompe a instância antiga, reinicia o serviço depois do
`wsl --shutdown` e só então remove os artefatos legados. O shutdown encerra
todas as distribuições WSL. Se uma etapa falhar, o backup fica em
`migration-backups` e a topologia anterior é restaurada quando possível.

## Exportação e importação

Exportações são JSON versionadas e removem hashes de senha, usuários de
autenticação, exposições temporárias e qualquer outro material de autenticação:

```powershell
devlan config export C:\Backups\devlan-config.json
devlan config import C:\Backups\devlan-config.json
```

Sem caminho, `config export` escreve no stdout. A importação substitui o
registro, valida a configuração e só então gera/recarrega Caddy e PHP-FPM. Se
a aplicação falhar, os arquivos gerados anteriores continuam sendo o rollback
válido. Credenciais e exposições devem ser configuradas novamente na máquina
de destino.

## Pacote de diagnóstico

Gere um único arquivo para suporte:

```powershell
devlan diagnostic
devlan diagnostic C:\Temp\devlan-support.zip
```

O ZIP inclui manifesto, configuração sem segredos, resultado do `doctor`,
runtime, o Caddyfile WSL único com credenciais mascaradas, estado de systemd,
mirrored networking, Hyper-V Firewall/CA e logs gerenciados disponíveis.
Ele não percorre projetos, não inclui `.env`, variáveis de ambiente, banco,
credenciais ou conteúdo de aplicação. O arquivo é criado com permissão privada
e deve ser enviado ao suporte por canal confiável.

## Telemetria opt-in

Não há coleta por padrão. Mesmo depois do consentimento, o envio é manual e
somente para um endpoint HTTPS informado pelo usuário:

```powershell
devlan telemetry enable https://telemetry.example.invalid/devlan
devlan telemetry status
devlan telemetry send
devlan telemetry disable
```

Eventos são reduzidos a nomes e atributos allowlisted; caminhos, hosts,
endereços IP, segredos e quebras de linha são descartados. Desabilitar remove
também a fila local. Um endpoint HTTP só é aceito para loopback, destinado a
testes ou coleta local.

## Atualização e recuperação

Um canal de atualização é escolhido explicitamente (`stable` ou `preview`) e
usa um manifesto HTTPS com `version`, `channel`, `url` e `sha256`:

```powershell
devlan update check stable https://updates.example.invalid/stable.json
devlan update download stable https://updates.example.invalid/stable.json C:\Temp\devlan.new.exe
```

O download é gravado em arquivo temporário, validado integralmente por
SHA-256 e só depois publicado no destino. O comando não substitui o executável
em execução: essa troca precisa ser feita por um instalador/serviço assinado.
O repositório ainda não contém certificado de assinatura nem pipeline de
release; portanto a assinatura do instalador e a substituição automática
continuam pendentes.

### Recuperação manual

1. Pare o serviço: `devlan service stop`.
2. Execute `devlan doctor` e preserve o ZIP produzido por `devlan diagnostic`.
3. Se a última alteração foi inválida, execute `devlan reload`; o núcleo usa
   os arquivos `.previous` gerenciados ao falhar durante a aplicação.
4. Restaure um JSON com `devlan config import` e execute `devlan reload`.
5. Se o serviço estiver corrompido, remova e instale novamente:
   `devlan service remove`, `devlan service install`.

`devlan uninstall` desfaz as integrações e arquivos de propriedade registrada,
incluindo firewall, token da API e fila de telemetria, restaura configurações
compartilhadas quando não houve alteração posterior e preserva diretórios dos
projetos. Use `devlan uninstall --dry-run` para revisar o plano; `--keep-data`
conserva o estado para uma reinstalação, `--keep-dependencies` mantém Caddy/PHP/
Composer/toolchains e `--purge --yes` trata resíduos legados selecionados.
Recursos sem proveniência são preservados por segurança. Em caso de interrupção
durante a remoção, `devlan service remove` e uma nova execução de
`devlan uninstall` são seguros; não remova manualmente a raiz de dados antes
de confirmar que os projetos estão fora dela. A restauração de `.wslconfig` ou
`/etc/wsl.conf` é acompanhada de instrução para executar `wsl --shutdown` ou
reiniciar a distribuição, pois o uninstall não interrompe outras distros sem
confirmação explícita.

## Matriz de validação

### GUI web-first e contratos

A GUI browser-first é a superfície canônica. O mesmo `DevLANClient` pode usar
HTTP, Wails ou o mock determinístico; a janela Wails continua opcional e apenas
preserva integração de tray/notificações e abertura da GUI.

Na pasta `frontend`, execute:

```powershell
npm ci
npm run validate
npm run build
npm run test:coverage
```

O contrato HTTP canônico está em `api/openapi.yaml`. Durante a transição, o
`internal/api/contract.json` é apenas a ponte da geração TypeScript; o R-07d
vai removê-la depois de validar a paridade dos geradores. Para regenerar os
tipos atuais após uma alteração deliberada:

```powershell
npm run contracts:generate
npm run contracts:check
```

O E2E usa as duas origens administrativas explicitamente configuradas:

```powershell
$env:DEVLAN_E2E_LOCAL_ORIGIN = 'https://devlan.localhost'
$env:DEVLAN_E2E_DIRECT_ORIGIN = 'http://127.0.0.1:3210'
npm run test:e2e
```

Sem essas variáveis, os testes E2E são pulados; os testes Go continuam cobrindo
history fallback, allowlist de Host/Origin, CSRF e separação da API.

O conjunto Go pode ser executado em todas as plataformas:

```powershell
go test ./...
go vet ./...
```

No Windows alvo, valide ainda o fluxo com WSL2/Ubuntu:

```powershell
devlan doctor
devlan status
devlan service status
devlan api status
```

Win10/Win11, WSL2 e Ubuntu devem ser validados no pipeline de release antes de
marcar a matriz como concluída. Os adaptadores não-Windows retornam erro
explícito para recursos específicos do SCM/Registro, em vez de simular uma
instalação.
