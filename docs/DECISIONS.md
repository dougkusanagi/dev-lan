# Decisões técnicas

## Go como núcleo

Go será usado para CLI, domínio, instalação, diagnósticos e processos. Motivos:

- executável único e distribuição simples no Windows;
- boa biblioteca padrão para HTTP, processos e concorrência;
- baixo tempo de inicialização;
- possibilidade de reutilizar o núcleo na interface Wails;
- cross-compilation para um pequeno agente Linux no WSL.

A CLI planejada para o WSL será um cliente fino do controlador Windows. Ela
resolverá caminhos Linux e a distribuição corrente, mas não possuirá banco ou
configuração independentes. Essa decisão evita divergência entre duas CLIs e
preserva uma única ordem para geração, validação, reload e rollback.

O domínio não deve importar Wails, Cobra, Caddy ou detalhes de PowerShell diretamente. Essas integrações ficam em adaptadores para permitir testes e evolução.

## Bootstrap e versão PHP do MVP

O bootstrap é PowerShell no Windows e usa um script Bash fixo no WSL. Go é
baixado da lista oficial de releases estáveis quando não está no PATH; Caddy
usa o pacote oficial/distribuição ou o release oficial com checksum. PHP não
tem uma modalidade LTS oficial: o MVP adota 8.5 por padrão, permite 8.3/8.4 e
detecta a versão efetivamente instalada. O instalador não executa `composer
install` nem qualquer script de projeto.

## Wails para a interface

Wails ocupa para Go um papel semelhante ao Tauri para Rust: backend nativo e interface feita com tecnologias web, renderizada pela WebView do sistema.

A interface será adicionada depois que o núcleo da CLI estiver estável. Ela usará:

- Wails;
- frontend web pequeno em TypeScript;
- Tailwind compilado;
- poucos componentes e estado local simples;
- bindings tipados gerados entre Go e TypeScript.

O objetivo não é criar outra implementação das regras. CLI e UI chamam os mesmos serviços do domínio.

Wails v3 oferece uma API moderna e suporte integrado a system tray, mas ainda está em beta em agosto de 2026. A versão deve ser fixada no projeto, testada no Windows-alvo e encapsulada atrás de um adaptador de UI. Se o beta se mostrar instável durante a fase de interface, a decisão deve ser revisitada sem afetar o núcleo.

Referências:

- https://v3.wails.io/
- https://v3.wails.io/features/menus/systray/
- https://wails.io/docs/introduction/

## Interface proposta

Uma janela pequena com:

- lista de projetos e estado;
- URL com ação de copiar/abrir;
- PHP ou runtime detectado;
- iniciar, parar e reiniciar;
- últimas linhas de log;
- diagnóstico e correções orientadas;
- configurações globais e sobrescritas do projeto.

O menu na tray terá apenas ações frequentes. Configuração detalhada fica na janela.

## Serviço Windows

O MVP não precisa começar como serviço permanente. O instalador pode realizar operações elevadas e configurar o Caddy, enquanto a CLI opera no usuário comum.

Se for necessário iniciar antes do login ou manter supervisão contínua, será criado um serviço Windows separado da interface. Serviços não desenham UI na sessão do usuário; Wails/tray será o cliente visual desse serviço.

## Dois Caddys

Manter dois Caddys preserva fronteiras:

- Windows: entrada LAN, endereço/porta, headers e política externa;
- WSL: document roots Linux, PHP-FPM, arquivos estáticos e upstreams JS.

O Caddy do Windows deve ter configuração quase estática. O do WSL pode ser regenerado conforme os projetos.

## Roteamento

O MVP usa `http://IP/nome` por ser a opção que não depende de DNS. O produto completo terá:

- `path`: simples, mas depende de suporte a base path;
- `port`: robusto para HMR e apps que assumem `/`;
- `host`: melhor experiência, requer DNS ou hosts distribuído.

A ferramenta deve recomendar o modo com base no tipo de projeto, sem alterar automaticamente uma escolha explícita.
