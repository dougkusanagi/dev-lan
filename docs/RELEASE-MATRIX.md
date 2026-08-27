# Matriz de release e smoke

| Eixo | Mínimo coberto | Smoke |
| --- | --- | --- |
| Windows | Windows 11 22H2+ x64, PowerShell 5+, Go da versão em `go.mod` | `scripts/smoke-release.ps1` |
| WSL | Ubuntu 22.04/24.04, WSL 2, Bash, systemd e `networkingMode=mirrored` | `devlan topology check` + cliente WSL |
| Caddy | uma instância systemd no WSL, com 80/443 e pool LAN | `DEVLAN_REAL_CADDY=1 go test ./internal/caddy -run TestRenderWSLUnifiedWithRealCaddy` + `scripts/test-m8-real.ps1` |
| PHP | 8.3, 8.4 e 8.5; PHP-FPM correspondente | fixture PHP + `devlan doctor` |
| Node | Node 20/22 e npm/pnpm conforme lockfile | fixture Vite/SSR + build da SPA |

O CI roda Go em Windows/Linux, `-race` no Linux, build/check/test do
frontend e validação real de Caddy. A matriz completa de PHP/WSL depende de um
runner Windows preparado; `scripts/test-m8-real.ps1` falha de forma acionável
quando o host não estiver em `single-wsl`, o Caddy não estiver vivo ou algum
endpoint não responder. Reexecute-o após reboot, VPN/troca de IP e, com a
confirmação explícita correspondente, após `wsl --shutdown`.
