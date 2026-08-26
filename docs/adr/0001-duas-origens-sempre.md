# ADR 0001 — Duas origens são invariantes

- **Status:** aceito
- **Data:** 2026-08-26

Todo projeto publicado pelo DevLAN possui simultaneamente uma origem local
`https://<nome>.localhost/` e uma origem LAN `http(s)://<ip>:<porta>/`. Não há
seletor de modo de roteamento: a porta LAN é uma propriedade operacional e a
origem `.localhost` é a superfície local canônica.

O sufixo `.localhost` é reservado para loopback e não depende de `hosts` ou
DNS. A porta LAN é persistida quando alocada; overrides explícitos continuam
sendo possíveis até a remoção dos modos legados no Marco 2.

Essa decisão evita que a escolha de uma origem quebre HMR, cookies, redirects
ou apps que assumem `/`, e torna a matriz de compatibilidade verificável.
