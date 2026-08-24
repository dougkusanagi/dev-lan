# DevLAN

DevLAN é uma ferramenta para publicar projetos que rodam no WSL para outros dispositivos da rede local, usando o Windows como ponto de entrada.

O primeiro caso de uso é simples:

```text
http://192.168.1.50/meu-projeto
```

O MVP será focado em projetos PHP, principalmente Laravel. Depois, a ferramenta evoluirá para múltiplas versões de PHP, sites estáticos, servidores JavaScript sob demanda, dashboard e menu na área de notificação do Windows.

## Princípios

- A CLI e a interface gráfica usam o mesmo núcleo em Go.
- O Windows controla a exposição na rede e o firewall.
- O WSL executa Caddy, PHP-FPM e os processos dos projetos.
- Configurações geradas são validadas antes de serem aplicadas.
- Cada projeto pode sobrescrever padrões globais.
- A primeira versão deve resolver bem Laravel/PHP antes de crescer.
- Projetos nunca são publicados sem terem sido registrados explicitamente por `link` ou `park`.

## Arquitetura resumida

```text
Dispositivo na LAN
       │
       ▼
Caddy no Windows :80 / :443 opcional
       │ localhost / integração WSL
       ▼
Caddy no WSL :8181
       │
       ▼
PHP-FPM ── projeto Laravel
```

No futuro, projetos JavaScript serão atendidos por um supervisor no WSL, que poderá iniciar o script `dev` no primeiro acesso e encerrar o processo depois de um período ocioso.

## Experiência esperada no MVP

```powershell
devlan install
devlan park /home/silver/dev
devlan link financeiro /home/silver/dev/financeiro
devlan status
devlan doctor
devlan secure
devlan open financeiro
```

Resultado:

```text
https://192.168.1.50/financeiro
```

Sem `devlan secure`, a URL permanece `http://192.168.1.50/financeiro`.

## Documentos

- [Arquitetura](docs/ARCHITECTURE.md)
- [CLI e configuração](docs/CLI-AND-CONFIG.md)
- [Instalação](docs/INSTALL.md)
- [Roadmap](docs/ROADMAP.md)
- [Decisões técnicas](docs/DECISIONS.md)

## Fora do escopo do MVP

- Interface gráfica ou menu na tray.
- Servidores JavaScript sob demanda.
- DNS interno e distribuição automática da CA para outros dispositivos.
- Múltiplas versões de PHP.
- Containers e bancos de dados.
- Instalação automática de dependências dos projetos.

## Implementação atual

O MVP está implementado como uma CLI Go em `cmd/devlan`. O núcleo cobre:

- registro explícito com `link` e descoberta de filhos Laravel por `park`;
- resolução `modo do projeto > park > padrão global`;
- detecção sem executar scripts, exigindo `artisan` e `public/index.php`;
- Caddyfiles Windows/WSL determinísticos, com rota por subpath e contexto
  FastCGI compatível com redirects e rotas Laravel;
- HTTPS opcional por CA interna com `devlan secure` e retorno a HTTP com
  `devlan unsecure`;
- aplicação por arquivo temporário, validação quando o Caddy está disponível e rollback da última configuração funcional;
- `install`, `uninstall`, `status`, `reload`, `doctor`, `logs`, `open` e os comandos de registro;
- regra de firewall DevLAN limitada ao perfil privado e à sub-rede local.

`devlan install` prepara os arquivos gerenciados e tenta criar a regra de firewall. Para uma máquina limpa, use o [bootstrap de instalação](docs/INSTALL.md): ele instala Go, WSL/Ubuntu, Caddy, PHP-FPM 8.5, extensões Laravel e Composer, compila a CLI e executa `devlan install`. Dependências dos projetos continuam explícitas; o bootstrap não executa `composer install` automaticamente.

## Instalação rápida via curl

Abra o **PowerShell como Administrador**. A elevação é necessária para instalar
ou configurar WSL, Caddy e a regra de firewall. Em seguida, execute:

```powershell
curl.exe -fsSL https://raw.githubusercontent.com/dougkusanagi/dev-lan/master/scripts/install.ps1 -o "$env:TEMP\devlan-install.ps1"; powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$env:TEMP\devlan-install.ps1"
```

O fluxo completo e as opções estão em [docs/INSTALL.md](docs/INSTALL.md).

## Desenvolvimento

Com Go instalado:

```powershell
go test ./...
go vet ./...
go run ./cmd/devlan help
```

Durante o desenvolvimento, `--data-dir` permite usar um diretório isolado:

```powershell
go run ./cmd/devlan --data-dir .devlan install
go run ./cmd/devlan --data-dir .devlan doctor
```
