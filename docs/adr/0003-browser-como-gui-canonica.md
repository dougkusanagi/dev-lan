# ADR 0003 — Browser é a GUI canônica

- **Status:** aceito
- **Data:** 2026-08-26

A interface web/API em uma porta administrativa local e em
`https://devlan.localhost/` é a superfície canônica da GUI. Wails e tray são
shells opcionais que abrem ou embutem a mesma superfície e não possuem outro
backend de domínio.

Essa fronteira permite operar o Core sem desktop nativo e mantém CLI, browser
e Wails sujeitos aos mesmos contratos, autenticação e coordenador.
