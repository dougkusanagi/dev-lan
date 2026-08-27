# Documentação do DevLAN

Este índice separa realidade atual, direção futura, trabalho pendente e
histórico. Para compreender o projeto sem carregar contexto excessivo, leia
nesta ordem:

1. [Estado atual](STATUS.md) — capacidades, limitações e trabalho ativo;
2. [Arquitetura](ARCHITECTURE.md) — desenho implementado e arquitetura alvo;
3. [Roadmap](ROADMAP.md) — única lista canônica de tarefas abertas;
4. [ADRs](adr/README.md) — decisões duráveis e seus motivos.

## Operação e referência

- [Instalação](guides/INSTALL.md)
- [Operações e troubleshooting](guides/OPERATIONS.md)
- [CLI e configuração](reference/CLI-AND-CONFIG.md)
- [Persistência e recuperação](reference/PERSISTENCE.md)
- [Execution plane WSL](reference/WSL-EXECUTION-PLANE.md)
- [Matriz de release](reference/RELEASE-MATRIX.md)

## Planos ativos

- [Refatoração Go orientada a testes](plans/GO-REFACTORING.md)
- [Performance e usabilidade](plans/PERFORMANCE-USABILITY.md)
- [Desinstalação reversível](plans/UNINSTALL.md)

Planos detalham estratégia e critérios técnicos. O estado das tarefas pertence
somente ao [roadmap](ROADMAP.md), evitando checklists divergentes.

## Histórico

`archive/` contém marcos concluídos, análises pontuais e planos substituídos.
Ele é útil para investigação e contexto de decisões, mas não descreve o estado
vigente e não deve ser carregado por agentes em tarefas comuns.
