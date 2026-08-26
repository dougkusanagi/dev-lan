# ADR 0004 — batching na fronteira WSL

- **Status:** aceito
- **Data:** 2026-08-26
- **Substitui:** nenhuma

## Decisão

O DevLAN mantém o `wsl.exe` como transporte do execution plane e agrupa as
operações que têm o mesmo ciclo de vida: descoberta, disponibilidade de
binários, sockets PHP, status dos processos dev e ACLs de projetos. A UI web
usa um read model agregado (`GET /api/v1/overview`) para materializar uma
fotografia coerente em um único polling.

Não será criado um agente Linux persistente neste marco. O benchmark do
runner isolado mede 16 spawns/operação no caminho direto contra 1 spawn/operação
no caminho agrupado. O ganho estrutural é material, enquanto um daemon
adicionaria lifecycle, handshake e uma segunda fronteira de disponibilidade
sem necessidade demonstrada. Uma medição real local com Ubuntu-26.04 também
reduziu a média de 3568,35 ms para 171,42 ms em rodadas de 16 comandos
(`~20,82x`), incluindo o overhead do PowerShell.

## Consequências

- O estado autoritativo continua no Windows; o cache de request idempotente é
  apenas uma janela em memória do controlador e não guarda estado de projeto.
- Cada chamada é cancelável por `context.Context` e recebe uma categoria
  estável (`unavailable`, `timeout`, `canceled` ou `execution`).
- Falhas transitórias de distribuição não ficam retidas no cache: o mesmo
  request idempotente pode ser tentado novamente depois que o WSL voltar.
- Um eventual agente futuro deverá trazer nova medição, ADR substitutivo e
  manter fallback direto para `wsl.exe`.
