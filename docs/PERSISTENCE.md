# Persistência e aplicação (Marco 1)

## Contrato

`config.toml` e `state.json` continuam sendo os arquivos operacionais, porém
agora pertencem a uma transação identificada por `manifest.json` e registrada
em `journal.jsonl`. A revisão monotônica fica no estado e é diferente da
versão de schema. `config.toml.previous` e `state.json.previous` são o último
par completo recuperável.

O lock `.lock` é adquirido antes de qualquer leitura que possa participar de
uma mutação. Ele é compartilhado entre CLI, API, Wails e serviço; o processo
também possui um mutex para serializar chamadas locais. Uma mutação baseada em
uma revisão antiga retorna conflito em vez de sobrescrever campos de outra
mutação.

## Pipeline

```text
plan → validate → stage → commit → reload → healthcheck → finalize
```

Falha antes do commit deixa a revisão anterior intacta. Falha depois do
commit restaura o par persistido, os artefatos gerados e tenta recarregar os
dois Caddys com a revisão anterior. Os resultados da aplicação distinguem
`applied`, `degraded`, `rolled_back` e `failed`.

## Migrações e testes

O schema atual é `ConfigSchemaVersion=1`, `StateSchemaVersion=1` e
`ManifestVersion=1`. Arquivos legados sem `schema_version` são migrados
explicitamente para 1; versões futuras são rejeitadas sem serem regravadas.
O export continua com envelope/versionamento próprio.

`Store.Fault` injeta falhas em journal, backup, manifesto e renames. Os testes
usam isso para simular interrupções entre os dois arquivos e confirmar que o
próximo load converge para um par completo.
