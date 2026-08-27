# ADR 0005 — Caddy único no WSL com rede espelhada

- Status: aceito para o Marco 8
- Substitui: [ADR 0001 — duas origens sempre](0001-duas-origens-sempre.md), somente na fronteira de execução da borda
- Data: 2026-08-26

## Contexto

O encaminhamento Windows → Caddy Windows → Caddy WSL exigia uma porta interna,
headers de identidade e dois ciclos de lifecycle. Isso duplicava TLS, health
checks e pontos de falha justamente na fronteira que precisa ser previsível.

## Decisão

Em Windows 11 22H2 ou posterior, com WSL 2, o DevLAN exige
`networkingMode=mirrored`, `firewall=true`, `dnsTunneling=true` e
`autoProxy=true` na seção `[wsl2]` do `.wslconfig`. O control plane, o estado,
a API local e a GUI continuam no Windows; toda a borda HTTP/HTTPS/LAN fica em
uma única instância Caddy executada e gerenciada por systemd no WSL.

O Caddy WSL escuta 80/443 quando TLS está configurado e somente as portas LAN
atribuídas aos projetos. A origem `.localhost` é loopback-only. O único
upstream Windows é o dashboard em `127.0.0.1:<ui_port>`; PHP-FPM, arquivos
estáticos, Vite/SSR, WebSocket e assets são atendidos no WSL.

O Windows coordena uma política mínima no Windows Firewall e no Hyper-V
Firewall (`Private`/`LocalSubnet`). O default inbound do Hyper-V permanece
`Block`; `ui_port` nunca é aberta na LAN. A CA privada permanece no WSL. O
Windows recebe apenas uma cópia validada do certificado raiz público para o
trust store; nenhuma chave privada cruza a fronteira.

## Consequências e migração

O instalador verifica build, WSL, systemd, loopback, LAN e conflitos de portas.
O editor de `.wslconfig` é transacional, preserva conteúdo desconhecido e cria
backup. Aplicar a alteração exige `wsl --shutdown`, que encerra todas as
distribuições: por isso `devlan topology migrate --yes` é explícito.

A migração valida e sobe o Caddy unificado antes de parar/remover a topologia
anterior, guarda os artefatos e restaura o backup se a etapa posterior falhar.
Artefatos da topologia dupla são lidos apenas para detecção e rollback durante
a janela de compatibilidade; novos reloads nunca geram nem iniciam Caddy no
Windows.
