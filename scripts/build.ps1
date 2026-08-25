param(
    [string]$OutputPath
)

$ErrorActionPreference = 'Stop'

$sourceRoot = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    $OutputPath = Join-Path $env:LOCALAPPDATA 'DevLAN\bin\devlan.exe'
}

$resolvedOutput = [IO.Path]::GetFullPath($OutputPath)
$outputDirectory = Split-Path -Parent $resolvedOutput

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go não foi encontrado no PATH.'
}

New-Item -ItemType Directory -Path $outputDirectory -Force | Out-Null

Push-Location $sourceRoot
try {
    $buildArgs = @(
        'build'
        '-tags'
        'desktop,production'
        '-ldflags'
        '-w -s -H windowsgui'
        '-trimpath'
        '-o'
        $resolvedOutput
        './cmd/devlan'
    )
    & go @buildArgs
    if ($LASTEXITCODE -ne 0) {
        throw "Falha ao compilar DevLAN (código $LASTEXITCODE)."
    }
}
finally {
    Pop-Location
}

Write-Host "DevLAN compilado em $resolvedOutput"
