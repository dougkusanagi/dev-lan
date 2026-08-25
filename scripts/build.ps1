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
$resourcePath = Join-Path $sourceRoot 'cmd\devlan\devlan_resource_windows_amd64.syso'
try {
    & go run ./cmd/devlan-resource -output $resourcePath
    if ($LASTEXITCODE -ne 0) {
        throw "Falha ao gerar o ícone Windows do DevLAN (código $LASTEXITCODE)."
    }

    $buildArgs = @(
        'build'
        '-tags'
        'desktop,production'
        '-ldflags'
        '-w -s'
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
    if (Test-Path -LiteralPath $resourcePath) {
        Remove-Item -LiteralPath $resourcePath -Force
    }
    Pop-Location
}

Write-Host "DevLAN compilado em $resolvedOutput"
