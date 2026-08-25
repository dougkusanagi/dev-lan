# Operação, recuperação e suporte

Este documento descreve os recursos operacionais da Fase 6 que já estão no
binário. O estado autoritativo continua em `%LOCALAPPDATA%/DevLAN` (ou no
valor de `DEVLAN_HOME`); os diretórios dos projetos nunca fazem parte de um
backup ou diagnóstico automático.

## Serviço Windows e inicialização

O serviço é opcional e independente da janela Wails. Ele inicia a API local,
recarrega a configuração gerenciada e permanece disponível mesmo quando a UI
está fechada:

```powershell
devlan service install
devlan service start
devlan service status
devlan service stop
devlan service remove
```

`service install` registra `DevLAN` no Service Control Manager com início
automático no boot. A operação exige um terminal elevado. A conta do serviço
não recebe acesso aos diretórios de projetos além das ACLs que o fluxo normal
já configura para Caddy/PHP-FPM.

Para iniciar apenas no login do usuário:

```powershell
devlan startup enable gui
devlan startup enable service
devlan startup status
devlan startup disable
```

O modo `service` da inicialização de login inicia o servidor de API em uma
sessão interativa quando o SCM não pode ser configurado; em instalações
normais, prefira o serviço Windows.
O valor é gravado somente em `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`.

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
GET  /v1/status
GET  /v1/projects
GET  /v1/config
POST /v1/reload
```

Para verificar o processo em execução:

```powershell
devlan api status
```

`api serve` é útil para desenvolvimento e para ambientes sem o serviço:

```powershell
devlan api serve
```

CLI e UI ainda podem chamar o núcleo Go diretamente; ambas as formas usam o
mesmo modelo e a mesma validação. A API é o contrato de transporte para o
serviço e para clientes futuros, sem expor a porta da LAN.

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
runtime, Caddyfiles com credenciais mascaradas e logs gerenciados disponíveis.
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
release; portanto a assinatura do instalador e a substituição automática são
explicitamente pendências da Fase 6.

### Recuperação manual

1. Pare o serviço: `devlan service stop`.
2. Execute `devlan doctor` e preserve o ZIP produzido por `devlan diagnostic`.
3. Se a última alteração foi inválida, execute `devlan reload`; o núcleo usa
   os arquivos `.previous` gerenciados ao falhar durante a aplicação.
4. Restaure um JSON com `devlan config import` e execute `devlan reload`.
5. Se o serviço estiver corrompido, remova e instale novamente:
   `devlan service remove`, `devlan service install`.

`devlan uninstall` remove firewall e arquivos gerenciados, incluindo token da
API e fila de telemetria, mas preserva diretórios dos projetos. Em caso de
interrupção durante a remoção, `devlan service remove` e uma nova execução de
`devlan uninstall` são seguros; não remova manualmente a raiz de dados antes
de confirmar que os projetos estão fora dela.

## Matriz de validação

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
