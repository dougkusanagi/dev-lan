# ADR 0002 — Windows controla; WSL executa

- **Status:** aceito
- **Data:** 2026-08-26

O estado autoritativo, a configuração, a coordenação de mutações e a API de
controle permanecem no Windows. O WSL é o execution plane para Caddy interno,
PHP-FPM e runtimes Linux; o cliente Linux encaminha comandos estruturados ao
controlador.

Um agente Linux persistente não faz parte da arquitetura por padrão. Ele só
será considerado se uma medição reproduzível mostrar ganho material sobre
batching de chamadas `wsl.exe`, sem criar um segundo estado.
