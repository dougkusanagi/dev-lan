# Plano de desinstalação reversível

## Objetivo

`devlan uninstall` deve desfazer a instalação do produto, e não apenas limpar
seu estado operacional. Ao terminar, tudo que o DevLAN criou ou modificou deve
ter sido removido ou restaurado, enquanto projetos, distribuições WSL e recursos
que já existiam permanecem intactos.

O comando atual para o Caddy, remove regras de firewall e apaga arquivos
gerenciados, mas não reverte `.wslconfig`/`wsl.conf`, não remove dependências
instaladas pelo bootstrap e não retira completamente binário/PATH e artefatos do
execution plane. O plano abaixo substitui esse contrato incompleto.

## Contrato da CLI

```text
devlan uninstall [--dry-run] [--keep-data] [--keep-dependencies] [--yes]
devlan uninstall --purge --yes
```

- O comportamento padrão remove integrações, estado e dependências cuja
  propriedade pelo DevLAN esteja comprovada, restaura configurações alteradas
  e sempre preserva os diretórios dos projetos.
- `--dry-run` produz exatamente o plano que seria executado, sem mutação.
- `--keep-data` conserva configuração, estado, logs, diagnósticos e backups do
  DevLAN para uma reinstalação que reutilize esses dados.
- `--keep-dependencies` conserva Caddy, PHP, Composer e toolchains, mas remove
  a configuração e as integrações específicas do DevLAN.
- `--purge` serve para resíduos legados sem manifesto. Ele mostra cada recurso
  não atribuível e exige `--yes` ou confirmação interativa. Nem `--purge` remove
  projetos ou executa `wsl --unregister`.
- Saída JSON expõe o plano e o resultado com estados `remove`, `restore`,
  `preserve`, `conflict`, `pending` e `failed`.

`uninstall` permanece idempotente: uma execução interrompida pode ser repetida
e uma execução posterior sobre uma máquina já limpa termina com sucesso.

## Manifesto de propriedade

O bootstrap deve criar um manifesto versionado antes da primeira mutação e
atualizá-lo atomicamente após cada etapa. Cada registro contém:

- tipo, localização e identidade forte do recurso;
- estado `preexisting`, `created`, `modified` ou `adopted`;
- hash/fingerprint anterior e posterior, quando aplicável;
- backup anterior e permissões/owner necessários para restauração;
- distribuição WSL e versão/SKU do pacote associado;
- etapa que criou o recurso e último resultado confirmado.

O manifesto não pode conter chave privada, token, senha ou conteúdo de projeto.
Backups sensíveis usam as mesmas restrições de acesso do estado atual.

Recursos compartilhados usam comparação em três estados: snapshot anterior,
valor aplicado pelo DevLAN e valor atual. O valor anterior só é restaurado se
o valor atual ainda for o aplicado pelo DevLAN. Divergência vira `conflict` e é
preservada para não sobrescrever uma alteração posterior do usuário.

## Inventário de remoção

### Windows

- parar e remover serviço, startup, tray/desktop e atalhos gerenciados;
- remover somente regras DevLAN do Windows Firewall e Hyper-V Firewall;
- remover do trust store apenas o certificado com thumbprint registrado;
- restaurar somente as chaves de `.wslconfig` alteradas pelo DevLAN, preservando
  outras seções, comentários e mudanças posteriores;
- remover entradas exatas de PATH e variáveis criadas pelo bootstrap;
- remover CLI, frontend incorporado, toolchain Go criado pelo bootstrap e raiz
  de dados, conforme as flags e a proveniência;
- usar um helper temporário para finalizar a remoção do executável que iniciou
  o comando e registrar qualquer limpeza adiada para o próximo login/reboot.

### WSL

- identificar a distribuição pelo manifesto, nunca assumir `Ubuntu`;
- parar/desabilitar Caddy e processos/pools gerenciados pelo DevLAN;
- remover `/usr/local/bin/devlan`, `/etc/devlan` e o Caddyfile publicado pelo
  DevLAN, validando identidade/hash antes;
- restaurar `/etc/wsl.conf`, PHP-FPM e outros arquivos compartilhados pela regra
  de três estados;
- remover Caddy, PHP, extensões e Composer somente se não existiam antes e foram
  instalados pelo bootstrap; nunca executar remoção ampla por glob ou um
  `autoremove` sem um conjunto exato registrado;
- remover a CA e os dados privados do Caddy somente quando pertencem à instância
  criada pelo DevLAN; preservar uma instalação Caddy adotada/preexistente;
- nunca remover nem desregistrar a distribuição WSL automaticamente.

Alterar `.wslconfig` pode exigir `wsl --shutdown`. O comando prepara a
restauração, informa que todas as distribuições serão interrompidas e só aplica
o shutdown com confirmação. Sem confirmação, termina com a etapa `pending` e
instrução acionável, sem declarar limpeza completa.

## Ordem e recuperação

1. Adquirir lock global e bloquear novas mutações do control plane.
2. Carregar/validar o manifesto e produzir o plano completo.
3. Parar consumidores antes de remover configuração ou credenciais.
4. Remover integrações de exposição: listeners, firewall, trust e startup.
5. Restaurar arquivos compartilhados e remover artefatos exclusivos no WSL.
6. Remover apenas dependências comprovadamente instaladas pelo bootstrap.
7. Remover dados conforme as flags e executar o helper de autolimpeza por último.
8. Verificar ausência/preservação esperada e emitir o resumo final.

Um journal durável registra cada transição antes e depois da mutação. Etapas
reversíveis mantêm backup até o healthcheck final; etapas não reversíveis só
ocorrem depois das validações e devem ser retomáveis de forma idempotente. Se
uma etapa falhar, o comando continua apenas nas limpezas independentes seguras,
preserva evidência para retry e termina com código diferente de zero.

## Instalações legadas

Instalações anteriores ao manifesto não recebem propriedade presumida. A
migração pode atribuir automaticamente somente recursos com identidade forte,
como regra de firewall com nome/especificação exatos, serviço DevLAN e arquivos
com formato/assinatura gerenciados. Pacote apt instalado, `.wslconfig` e arquivos
compartilhados permanecem por padrão quando sua origem não puder ser provada.

`--purge` permite selecionar esses resíduos conscientemente, mostra o motivo da
incerteza e captura um backup antes de qualquer restauração destrutiva. O resumo
não usa a expressão "desinstalação completa" enquanto houver `conflict`,
`pending` ou `failed`.

## Testes e critérios de aceitação

- testes de tabela para cada recurso e transição do planner;
- fault injection antes/depois de cada mutação, seguida de retry;
- Caddy/PHP/Composer preexistentes versus instalados pelo bootstrap;
- edição do usuário após install em `.wslconfig`, `wsl.conf` e PHP-FPM;
- regras/certificados ausentes, substituídos ou parcialmente removidos;
- distro parada, ausente, renomeada e mais de uma distro instalada;
- `--dry-run` sem writes e paridade exata entre plano e execução;
- verificação de que nenhum caminho de projeto é alvo de remoção;
- duas execuções consecutivas com o mesmo resultado convergente;
- instalação limpa → uso → uninstall → bootstrap → `doctor`, incluindo
  smoke real após reboot e `wsl --shutdown`.

O gate fecha quando a matriz prova tanto a ausência de resíduos DevLAN quanto a
preservação byte a byte ou semântica dos recursos preexistentes e dos projetos.
