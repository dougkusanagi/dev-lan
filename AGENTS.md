# Guia de trabalho para agentes

Leia apenas o necessário para a tarefa. A rota padrão é:

1. `docs/STATUS.md` para saber o que existe e o que está em andamento;
2. `docs/ARCHITECTURE.md` para limites, fluxos e arquitetura alvo;
3. `docs/ROADMAP.md` para tarefas ainda abertas;
4. um guia, referência, plano ou ADR específico somente se a tarefa exigir.

`docs/archive/` é histórico e não deve entrar no contexto normal. Código e testes
prevalecem se uma descrição histórica divergir do produto.

## Invariantes do produto

- O Windows mantém o control plane e o estado autoritativo.
- O WSL 2 com rede espelhada e systemd é o execution plane.
- Há um único Caddy, executado no WSL.
- Todo projeto tem duas origens simultâneas: `https://nome.localhost/` e uma
  porta LAN persistente. Não reintroduza modo de roteamento, hostname LAN ou
  publicação por subpath.
- A GUI canônica é web/browser-first. Wails é apenas um shell opcional.
- O cliente Linux é fino e não mantém um segundo domínio ou estado concorrente.
- Projetos do usuário e recursos preexistentes nunca são removidos por inferência.
- Atribuição de métricas usa metadado confiável do Caddy, nunca a URI solicitada.

## Limites arquiteturais

- Domínio não importa API, GUI, CLI, sistema operacional nem persistência.
- Casos de uso dependem de interfaces pequenas, declaradas pelo consumidor.
- Adaptadores implementam filesystem, processos, WSL, Caddy, firewall, trust
  store e persistência; decisões de negócio não ficam nesses adaptadores.
- CLI, HTTP e Wails chamam os mesmos casos de uso e não acessam `config.Store`
  diretamente.
- DTOs de transporte não são modelos de domínio. Evite `any` em fronteiras.
- Prefira injeção manual de dependências e biblioteca padrão; uma dependência
  nova precisa eliminar complexidade real e deve ser registrada quando alterar
  uma decisão durável.

## Forma de trabalhar

- Antes de mover comportamento, escreva ou confirme testes de caracterização.
- Em refatoração: teste verde, mudança pequena, teste verde. Não misture uma
  reorganização mecânica com alteração funcional no mesmo passo.
- Preserve alterações existentes do usuário e mantenha compatibilidade de CLI,
  API, arquivos persistidos e Caddy gerado, salvo mudança explicitamente pedida.
- Corrija a causa, não apenas o teste. Testes de integração dependentes de
  Windows/WSL real devem ser opt-in e documentar o ambiente.
- Use `gofmt` e mantenha arquivos Go focados. O pacote, não o arquivo, é a
  unidade de encapsulamento; dividir arquivos é uma primeira etapa segura, não
  a arquitetura final.

## Gates locais

Na raiz do repositório:

```text
go test ./...
go vet ./...
go test -race ./...
cd frontend && npm run validate && npm run build
```

O `-race` e a matriz Windows+WSL podem depender do ambiente de execução. Se não
forem executados, registre isso claramente no handoff.

## Manutenção da documentação

- `docs/ROADMAP.md` contém apenas tarefas abertas e seus critérios de aceite.
- Ao concluir uma tarefa, remova-a do roadmap e registre o resultado resumido
  em `docs/archive/ROADMAP-COMPLETED.md`.
- Planos explicam execução; não mantêm uma segunda checklist concorrente.
- `docs/STATUS.md` deve permanecer curto e verificável.
- ADRs são imutáveis após aceitos; uma mudança cria outro ADR que substitui o
  anterior.
- Atualize documentação junto da mudança que altera contrato, topologia ou
  operação. Não duplique detalhes descobríveis diretamente no código.
