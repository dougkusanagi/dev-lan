# ADR-0008 — Execução por usuário quando o runtime depende do WSL

## Status

Aceito

## Contexto

O control plane Windows precisa invocar uma distribuição WSL registrada para o
usuário interativo. Um serviço criado no Service Control Manager sem `obj=` é
executado como `LocalSystem`. Essa conta pode iniciar a API, mas não
necessariamente enxerga a distribuição, Caddy, PHP-FPM ou os parks do usuário.

O resultado é uma API aparentemente saudável que marca o execution plane como
indisponível, não descobre projetos estacionados e pode competir com o agente
iniciado pelo usuário. O binário compilado não determina essa conta; o registro
do serviço no Windows determina.

## Decisão

Para o runtime WSL por usuário, o modo padrão é um agente iniciado no login do
usuário (`HKCU`), executando `devlan api serve`. A instalação de serviço SCM é
opcional e exige confirmação explícita (`devlan service install --system`).

O serviço SCM executa um preflight de acesso ao WSL antes de abrir a API. Se a
conta não puder acessar a distro configurada, ele falha sem publicar um
endpoint administrativo incompleto.

Com uma API ativa, comandos mutáveis que possuem endpoint conhecido devem ser
encaminhados ao controlador autenticado. A CLI só compõe um controlador direto
em modo de recuperação, quando não existe API saudável.

## Consequências

- A instalação normal não exige credenciais de serviço nem armazena senhas.
- A inicialização é compatível com a identidade que possui o WSL.
- Ambientes realmente sistêmicos continuam podendo usar `--system`.
- Migração de uma instalação SCM existente exige elevação para parar ou
  desabilitar o serviço, mas preserva todo o estado e os diretórios de projetos.
- O overview mantém entradas baseadas em alocações persistidas quando a
  descoberta temporariamente falha, marcando a infraestrutura como degradada.
