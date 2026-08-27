$ErrorActionPreference = 'Stop'

# Fast architectural guard: inner packages must not import transports or cmd.
$files = Get-ChildItem -Path (Join-Path $PSScriptRoot '..\internal') -Recurse -Filter '*.go'
foreach ($file in $files) {
    $text = Get-Content -LiteralPath $file.FullName -Raw
    if ($file.FullName -match '\\internal\\domain\\' -and $text -match 'internal/(api|gui|platform|config)') {
        throw "domain import boundary violated: $($file.FullName)"
    }
    if ($file.FullName -match '\\internal\\application\\' -and $text -match 'internal/(api|gui|cmd)') {
        throw "application import boundary violated: $($file.FullName)"
    }
}
Write-Output 'dependency boundaries: OK'
