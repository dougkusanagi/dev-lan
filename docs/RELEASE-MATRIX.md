# Matriz de release e smoke

| Eixo | Mínimo coberto | Smoke |
| --- | --- | --- |
| Windows | Windows 10/11 x64, PowerShell 5+, Go da versão em `go.mod` | `scripts/smoke-release.ps1` |
| WSL | Ubuntu 22.04/24.04, `wsl.exe`, Bash | `devlan doctor` + cliente WSL, quando disponível |
| Caddy | versão instalada pelo bootstrap e duas instâncias independentes | `DEVLAN_RUN_CADDY_TESTS=1 go test ./internal/caddy -run TestRealCaddyPair` |
| PHP | 8.3, 8.4 e 8.5; PHP-FPM correspondente | fixture PHP + `devlan doctor` |
| Node | Node 20/22 e npm/pnpm conforme lockfile | fixture Vite/SSR + build da SPA |

O CI roda Go em Windows/Linux, `-race` no Linux, build/check/test do
frontend e validação real de Caddy. A matriz completa de PHP/WSL depende de um
runner Windows preparado; o script falha de forma acionável quando um
componente opcional não estiver instalado.
