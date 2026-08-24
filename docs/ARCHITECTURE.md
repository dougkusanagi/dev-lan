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

### Caddy no Windows

É a borda da rede. Escuta somente nos endereços e portas configurados, recebe requisições da LAN e as encaminha para o WSL.

Sua configuração deve mudar pouco. A lógica de PHP e de cada projeto permanece no WSL.

### Caddy no WSL

Conhece os projetos, seus document roots, modos de atendimento e sockets PHP-FPM. A CLI gera fragmentos a partir do registro de projetos e só recarrega o Caddy após `caddy validate` ter sucesso.

### PHP-FPM

No MVP haverá uma versão de PHP e um pool compartilhado, usando:

```ini
pm = ondemand
pm.max_children = 10
pm.process_idle_timeout = 10s
pm.max_requests = 500
```

O processo mestre permanece ativo, mas workers ociosos são encerrados. Versões e pools por projeto entram depois do MVP.

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

## Compatibilidade com Laravel em subpath

Servir Laravel em `/nome` exige cuidado porque redirects, assets, cookies e URLs absolutas podem assumir `/`.

No MVP, o adaptador Laravel deve:

- exigir `public/index.php` como document root;
- fornecer `X-Forwarded-Host`, `X-Forwarded-Proto` e `X-Forwarded-Prefix`;
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
  state.db
  generated/Caddyfile.windows
  logs/

/etc/devlan/
  generated/Caddyfile
  snippets/
  backups/
```

- TOML guarda preferências editáveis.
- SQLite guarda projetos, portas, processos e migrações de schema.
- Arquivos em `generated` não devem ser editados manualmente.
- Personalizações entram por snippets com pontos de extensão definidos.

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
