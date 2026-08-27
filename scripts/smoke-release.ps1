$ErrorActionPreference = 'Stop'

Write-Host 'DevLAN release smoke'
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Go não encontrado.' }
if (-not (Get-Command node -ErrorAction SilentlyContinue)) { throw 'Node não encontrado.' }
if (-not (Get-Command npm -ErrorAction SilentlyContinue)) { throw 'npm não encontrado.' }

go test ./... -count=1
go vet ./...
Push-Location (Join-Path $PSScriptRoot '..\frontend')
try {
    npm ci
    npm run build
    npm run check
} finally { Pop-Location }

$optional = @('caddy', 'wsl.exe', 'php', 'php-fpm', 'node')
foreach ($command in $optional) {
    $found = Get-Command $command -ErrorAction SilentlyContinue
    if ($found) { Write-Host "[ok] ${command}: $($found.Source)" }
    else { Write-Warning "[degraded] ${command} não encontrado; smoke de integração foi omitido." }
}
Write-Host 'Smoke de release concluído.'
