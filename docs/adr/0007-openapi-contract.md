# ADR-0007 — OpenAPI como contrato HTTP canônico

**Estado:** aceito 2026-08-27

O contrato HTTP passa a ter uma especificação OpenAPI versionada em
`api/openapi.yaml`. A manifestação existente em `internal/api/contract.json`
continua sendo a fonte intermediária da geração TypeScript durante a migração;
nenhum endpoint é removido nesta fase. A adoção de geradores completos fica
condicionada a uma etapa posterior que valide paridade de rotas e schemas.

Essa transição mantém compatibilidade com clientes existentes e torna explícita
a direção para consolidar DTOs, sem introduzir dependência de runtime ou uma
migração big-bang.
