# Plano de reimplementação da interface web

> A execução deste plano é acompanhada principalmente pelas tarefas `M5-*` e
> `M6-*` em [ROADMAP.md](ROADMAP.md). Este documento detalha o desenho; o
> roadmap é a tasklist canônica.

## Objetivo

Recriar o frontend como SPA browser-first, preservando o núcleo Go, a CLI e o
estado atual como fontes autoritativas. A mesma aplicação será acessível por
`https://devlan.localhost/` e pela porta administrativa local. Wails/tray pode
permanecer como shell opcional, mas não como backend paralelo. A interface deve
adotar a linguagem visual das referências: uma ferramenta escura, compacta e
centrada no projeto selecionado.

As imagens mostram a mesma página em posições diferentes de rolagem:

- no topo: navegação, endereço do projeto, runtimes, serviços e indicadores;
- mais abaixo: gráficos, rotas lentas e tabela de rotas.

A referência orienta composição, densidade e hierarquia. Recursos que aparecem
nas imagens, mas não existem no DevLAN, não entram automaticamente no produto.
Toda mutação passa pela API HTTP versionada, que chama `internal/app.App` com
as mesmas validações e rollback da CLI. O frontend nunca recebe o token de API
persistido em disco.

## Princípios da adaptação

- preservar a identidade do DevLAN; a referência não deve ser copiada como uma
  reprodução de marca;
- mostrar somente estados e ações respaldados por dados reais;
- priorizar leitura rápida: status, URL e ações principais devem ficar visíveis
  sem abrir modal;
- manter o contexto do projeto enquanto o conteúdo central rola;
- usar painéis e drawers para tarefas recorrentes; reservar modal para
  confirmação, credencial ou operação destrutiva;
- não executar comandos da CLI por shell a partir do frontend.
- tratar browser, Wails e mocks como transports de um único `DevLANClient`;
- manter API same-origin, proteção CSRF e política estrita de Host/Origin.

## Escopo inicial

### Incluído

- novo shell com rail lateral, sidebar de sites e área principal;
- entrega da SPA e API nas URLs `devlan.localhost` e `127.0.0.1:ui_port`;
- busca, agrupamento, seleção e status dos projetos;
- visão geral do projeto com URLs local e LAN, runtime, saúde e ações rápidas;
- abrir/copiar cada URL, iniciar/parar/reiniciar, build, dependências, logs, reload,
  TLS e diagnóstico;
- configurações globais e overrides por projeto;
- operações restantes de PHP, segurança, exportação/importação, telemetria e
  atualização que já existem na CLI;
- estatísticas locais de requisição, caso possam ser obtidas por access log
  estruturado sem introduzir infraestrutura analítica;
- tema escuro como padrão, estados vazios/erro/carregamento e navegação por
  teclado.

### Fora do primeiro corte

- novos runtimes, bancos ou comandos de domínio;
- contas, colaboração, métricas remotas ou banco analítico;
- integração Git apenas para reproduzir o nome de branch da referência;
- PostgreSQL, Mailpit, Redis e RustFS sem detecção real no backend;
- Env, Tinker e Depuração sem uma operação equivalente no núcleo.

Esses elementos devem ser omitidos, em vez de aparecer como botões inativos.
As abas iniciais serão `Visão geral` e `Logs`; configurações e diagnóstico
ficarão em painéis próprios. Novas abas só entram quando tiverem conteúdo real.

## Anatomia da tela

### Estrutura fixa

```text
┌──────┬─────────────────────┬──────────────────────────────────────────────┐
│ rail │ sidebar de sites    │ contexto do projeto                         │
│ 56px │ 255px aproximados   ├──────────────────────────────────────────────┤
│      │                     │ barra de endereço e ações                   │
│      │                     ├──────────────────────────────────────────────┤
│      │                     │ abas                                        │
│      │                     ├──────────────────────────────────────────────┤
│      │ rolagem própria     │ conteúdo com rolagem própria                │
└──────┴─────────────────────┴──────────────────────────────────────────────┘
```

- viewport de referência próxima de `1080 × 1000`, também validada no browser;
- rail e sidebar ocupam toda a altura e não rolam junto com o conteúdo;
- cabeçalho do projeto, barra de endereço e abas permanecem fixos;
- somente o corpo da aba rola, com uma scrollbar fina no extremo direito;
- largura mínima funcional de 1100px; abaixo disso, a sidebar pode ser
  recolhida e os cartões passam para uma coluna.

### Rail lateral

- marca do DevLAN no topo;
- destinos primários representados por ícone, com item ativo destacado;
- divisor entre navegação e utilitários;
- documentação, notificações, tema e versão na base;
- tooltip obrigatório em todo botão que tiver apenas ícone.

Não reproduzir ícones sem função. No primeiro corte, a rail pode conter apenas
`Sites`, `Diagnóstico`, `Configurações`, `Documentação` e `Tema`.

### Sidebar de sites

- título `SITES`, ação de adicionar e busca acessível por `Ctrl+K`;
- grupos recolhíveis derivados do estado real: projetos vinculados, cada pasta
  estacionada e projetos pausados/ocultos quando aplicável;
- linha de projeto entre 40px e 44px, com nome, framework, status e indicadores
  úteis como TLS, modo e runtime;
- seleção com fundo vermelho escuro translúcido e nome em vermelho claro;
- estados `Pronto`, `Iniciando`, `Parado`, `Degradado` e `Erro` distinguíveis
  por cor e ícone, nunca somente por cor;
- ações secundárias no menu de contexto para não poluir cada linha;
- rodapé com adicionar, filtros e contagem de projetos.

Se o backend ainda não informar a pasta estacionada de origem, ampliar o DTO
de leitura para fornecê-la; não deduzir grupos por caminho no frontend.

### Cabeçalho do projeto

O topo da referência será adaptado em três faixas:

1. **Contexto:** nome do projeto selecionado e botão para abrir outro contexto.
   Branch Git só aparece futuramente se o backend passar a fornecê-la.
2. **Endereços:** `https://projeto.localhost/` como origem local principal e
   `http(s)://IP:porta/` como origem LAN, ambas copiáveis, com badges claros,
   estado de TLS/firewall e ações de abrir, copiar, reload e logs. Não há
   seletor de modo de rota, hostname customizado ou subpath.
3. **Abas:** `Visão geral` e `Logs`, com sublinhado vermelho no item ativo;
   caminho local alinhado à direita quando houver espaço.

A URL local é o elemento visual dominante; a URL LAN permanece visível como
segunda origem. Ações destrutivas ficam no menu adicional e exigem confirmação.

## Visão geral

O corpo deve seguir exatamente a ordem visual percebida nas referências.

### 1. Runtime e workers

- título pequeno em caixa alta;
- seletores compactos para versão PHP e runtime JavaScript quando aplicáveis;
- pills de processo para servidor dev e workers conhecidos pelo DevLAN;
- ponto verde/âmbar/vermelho acompanhado do texto do estado;
- controles ausentes no projeto não deixam espaços vazios artificiais.

### 2. Serviços

- grid de até três cartões por linha;
- ícone, nome e estado em duas linhas;
- mostrar Caddy WSL único, estado de rede espelhada, PHP-FPM ou servidor dev, pois já são
  observáveis pelo DevLAN;
- serviços externos só entram quando houver contrato real de detecção/status.

### 3. Tempo de requisição

- cabeçalho da seção com seletor segmentado `15m`, `1h`, `24h` e `7d`;
- quatro cartões em grade 2 × 2: `Típico (p50)`, `P95`, `Requisições` e
  `Taxa de erros`;
- valor grande, unidade menor e legenda contextual;
- erro usa âmbar ou vermelho conforme severidade;
- nota abaixo dos cartões para amostras excluídas, como cold starts, quando
  essa exclusão realmente ocorrer.

### 4. Gráficos

- duas colunas: histograma `Tempo de resposta` e linha/área
  `Requisições por minuto`;
- histograma com buckets `<25ms`, `<50ms`, `<100ms`, `<250ms`, `<500ms`,
  `<1s` e `>1s`;
- verde para faixas rápidas, âmbar para intermediárias e vermelho para lentas;
- série temporal com eixo mínimo, início/fim do intervalo e unidade `req/min`;
- preferir SVG e CSS próprios para estes dois gráficos simples, evitando uma
  dependência pesada somente para reproduzir a referência.

### 5. Rotas mais lentas

- painel de largura total;
- até cinco rotas ordenadas por p95 decrescente;
- badge do método HTTP, caminho normalizado, barra relativa e latência;
- vermelho para a pior faixa, âmbar para atenção e verde para aceitável;
- ação discreta para abrir detalhes quando houver dados suficientes.

### 6. Tabela de rotas

- painel de largura total com abas `Rotas` e `Requisições recentes`;
- colunas iniciais: rota, p50, p95, latência relativa e requisições;
- linhas compactas, método HTTP em badge e números alinhados à direita;
- ordenação por coluna e seleção de linha por teclado;
- `Requisições recentes` só aparece quando os registros puderem ser mostrados
  sem IP, query string, corpo, cookies ou credenciais.

## Linguagem visual

### Tokens iniciais

| Uso | Direção |
| --- | --- |
| Fundo da janela | quase preto (`#0d0e10`) |
| Rail/sidebar | preto elevado (`#121315`) |
| Cartão | cinza muito escuro (`#171719`) |
| Borda | branco com 8% a 10% de opacidade |
| Texto principal | branco frio (`#f5f7fa`) |
| Texto secundário | cinza azulado (`#8b97aa`) |
| Acento | vermelho DevLAN (`#ff342e`) |
| Sucesso | verde/teal (`#00c58e`) |
| Atenção | âmbar (`#ffa500`) |
| Erro | vermelho/rosa (`#ff3045`) |

- fonte de interface compatível com o sistema; fonte monoespaçada para URL,
  caminho, branch e rotas;
- raio entre 6px e 9px, sem cartões excessivamente arredondados;
- espaçamento base de 4px e densidade compacta;
- labels de seção em 11px, conteúdo em 12px–14px e métricas em 24px–28px;
- sombras mínimas; separação baseada principalmente em borda e contraste;
- animações entre 120ms e 180ms, respeitando `prefers-reduced-motion`.

## Componentes e organização do frontend

Substituir o `App.tsx` monolítico. Reaproveitar apenas os contratos/adaptadores
que continuarem válidos; não migrar a árvore visual existente.

```text
frontend/src/
  app/
    AppShell.tsx
    routes.ts
  components/
    rail/
    sidebar/
    project-header/
    metrics/
    feedback/
  features/
    projects/
    logs/
    doctor/
    settings/
    php/
    security/
    import-export/
    telemetry/
    updates/
  hooks/
  services/devlan-api.ts
  types/
  styles/tokens.css
```

Componentes principais: `ActivityRail`, `ProjectSidebar`, `ProjectGroup`,
`ProjectListItem`, `ProjectHeader`, `AddressBar`, `ProjectTabs`,
`RuntimeToolbar`, `ServiceCard`, `MetricCard`, `LatencyHistogram`,
`TrafficChart`, `SlowRoutes`, `RoutesTable`, `LogsPanel`, `SidePanel`,
`ConfirmDialog`, `EmptyState`, `Skeleton` e `ToastStack`.

## Estado e atualização

Separar o estado em:

1. **Dados do núcleo:** projetos, configuração, infraestrutura, métricas e
   resultados vindos do `DevLANClient`.
2. **Navegação:** projeto selecionado, aba, busca, grupos expandidos, intervalo
   e painel lateral aberto.
3. **Operações:** ação em andamento, progresso, confirmação, erro e toast.

Regras:

- preservar projeto, aba, posição de rolagem e grupos durante polling;
- atualizar lista/status em ritmo diferente das métricas;
- ignorar respostas antigas quando projeto ou intervalo mudar;
- atualizar otimisticamente apenas ações reversíveis;
- bloquear somente o controle em operação, nunca toda a janela;
- persistir tema, sidebar recolhida, último projeto e último intervalo.

## Contrato de estatísticas

```ts
type MetricsRange = '15m' | '1h' | '24h' | '7d';

interface MetricsSnapshot {
  project: string;
  range: MetricsRange;
  generatedAt: string;
  excludedColdStarts: number;
  requests: number;
  requestsPerMinute: number;
  errorCount: number;
  errorRate: number;
  p50Ms: number | null;
  p95Ms: number | null;
  latencyBuckets: { upperBoundMs: number | null; count: number }[];
  traffic: { at: string; requestsPerMinute: number }[];
  routes: {
    method: string;
    normalizedPath: string;
    p50Ms: number | null;
    p95Ms: number | null;
    requests: number;
    errors: number;
  }[];
}
```

Valores ausentes renderizam `Sem dados`, nunca zero ou gráfico inventado.

O código atual não expõe métricas HTTP nem configura access log estruturado no
Caddy. A implementação de menor custo é:

1. adicionar access log JSON sanitizado às rotas geradas;
2. agregar janelas por projeto no núcleo, sem banco de dados;
3. normalizar parâmetros de rota, por exemplo `/orders/:id`;
4. expor `GET /api/v1/projects/{project}/metrics?range=...`;
5. limitar retenção e tamanho dos arquivos gerenciados.

Não armazenar IP, query string, corpo, headers, cookies ou credenciais. Se essa
coleta não couber no primeiro corte, a visão geral entrega runtime e serviços,
e a seção de métricas mostra um único estado vazio explicando que a coleta
local não está habilitada.

## Paridade da API web com a CLI

Ampliar o contrato HTTP tipado de `services/devlan-api.ts` e os handlers Go
antes de construir cada formulário. O adapter Wails temporário delega ao mesmo
contrato e não ganha métodos exclusivos.

| Domínio | Operações que a UI deve alcançar |
| --- | --- |
| PHP | listar, instalar, remover, selecionar versão, extensões, pools, preset, informações e ambiente do Composer |
| Segurança | porta/firewall LAN, exposição temporária, allowlist, autenticação HTTP, CA local, postura e auditoria |
| Configuração | exportar e importar JSON sanitizado, confirmando aplicação/reload |
| Telemetria | status, consentir/habilitar, desabilitar e enviar manualmente |
| Atualização | consultar `stable`/`preview`, mostrar manifesto/hash e preparar download verificado |

- handlers HTTP chamam o núcleo e usam timeout/cancelamento equivalente à CLI;
- operações privilegiadas sinalizam elevação antes de começar;
- importação mostra resumo e confirmação antes de substituir o estado;
- atualização deixa claro que o artefato é somente preparado, não instalado;
- segredos nunca retornam ao frontend depois de enviados;
- endpoints novos recebem testes Go, contract tests e mocks de browser;
- GET é somente leitura; mutações exigem sessão e token anti-CSRF;
- Host/Origin desconhecidos e versões incompatíveis são rejeitados.

## Etapas de execução

### 1. Fundação visual e contratos

- registrar tokens, dimensões e estados deste documento;
- criar os novos diretórios e uma shell vazia, removendo a composição antiga;
- criar `DevLANClient`, adapter HTTP e fake; manter Wails temporariamente atrás
  dessa interface;
- servir a shell em ambas as origens administrativas com assets versionados;
- criar fixtures para PHP, JavaScript, estático, parado, degradado e sem
  métricas;
- preparar capturas determinísticas em `1080 × 1000`.

**Aceite:** o modo mock renderiza rail, sidebar, cabeçalho e estados básicos
sem depender do backend real.

### 2. Navegação e contexto do projeto

- implementar rail, grupos, busca, seleção e menu de contexto;
- implementar as três faixas fixas do cabeçalho;
- conectar as duas URLs, TLS, firewall, abrir, copiar, reload e seleção persistente;
- implementar `Visão geral` e `Logs`.

**Aceite:** navegar e atualizar dados não perde seleção, aba ou rolagem; a
captura reproduz a hierarquia e densidade da parte superior da referência.

### 3. Visão geral operacional

- implementar runtime/workers e cartões dos serviços reais;
- conectar start/stop/restart, build, dependências, logs e doctor;
- criar skeletons e estados vazio, degradado e erro;
- validar o comportamento com projetos PHP, dev e static.

**Aceite:** o topo da visão geral informa o que está rodando e oferece somente
ações válidas para o projeto selecionado.

### 4. Estatísticas

- implementar coleta sanitizada e agregação local, se o custo permanecer
  pequeno;
- construir indicadores, histograma, série temporal, rotas lentas e tabela;
- aplicar seletor de intervalo e atualização independente;
- validar percentis, contagens, ordenação e ausência de dados sensíveis.

**Aceite:** a parte inferior corresponde à estrutura da referência, todos os
números são derivados de amostras reais e o estado sem coleta é honesto.

### 5. Operações restantes

- ampliar a ponte e criar painéis de PHP e segurança;
- criar fluxos de exportação/importação, telemetria e atualização;
- mostrar herança global/projeto e o valor efetivo nos formulários;
- confirmar remoção, exposição, rotação da CA, importação e download.

**Aceite:** as tarefas de paridade correspondentes no roadmap podem ser
marcadas como concluídas; cada operação listada tem paridade real com a CLI.

### 6. Qualidade e acabamento

- atalhos de busca, troca de projeto, refresh e fechamento de painéis;
- foco visível, contraste, leitor de tela e redução de movimento;
- testes de componentes, handlers HTTP, segurança e contract tests Go/TS;
- validar 1100px, 1366px e 1920px, além da sidebar recolhida;
- comparar capturas do modo mock com as duas referências.

**Aceite:** build TypeScript e testes Go passam; a interface continua útil
quando o shell Wails, Caddy, WSL ou a coleta de métricas estão indisponíveis.

## Ordem de entrega

1. fundação visual e contratos;
2. shell, sidebar e cabeçalho;
3. visão geral com dados existentes;
4. estatísticas locais, se viáveis no corte;
5. paridade das operações restantes;
6. acessibilidade, responsividade e comparação visual.

Essa ordem valida cedo o desenho das referências e mantém estatísticas como uma
extensão isolada, sem bloquear a reimplementação principal.
