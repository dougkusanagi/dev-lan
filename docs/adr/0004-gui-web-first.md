# ADR 0004 — Interface web canônica e shell desktop opcional

## Status

Aceito no M6.

## Decisão

O navegador continua sendo a superfície canônica do DevLAN. A SPA e a API
local são a referência de comportamento, contratos, segurança, acessibilidade
e capturas determinísticas. O Wails permanece opcional somente como shell: ele
abre a mesma superfície e delega as operações ao adapter Wails que implementa
o mesmo `DevLANClient`; não existe uma segunda árvore de domínio.

Tray, notificações e inicialização com login continuam preservados porque são
funções de integração do Windows que não precisam duplicar a GUI. Qualquer
diferença entre o shell e o navegador bloqueia o build de integração até que o
contrato web seja corrigido.

## Consequências

- testes de componentes e E2E validam a web antes do shell;
- HTTP, Wails e mock passam pelos mesmos validadores de contrato;
- o desktop pode ser instalado/removido sem ser requisito do Core;
- novos métodos de domínio devem ser adicionados ao `DevLANClient`, nunca
  diretamente a uma tela Wails.
