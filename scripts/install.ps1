[CmdletBinding()]
param(
    [ValidatePattern('^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$')]
    [string]$Repository = 'dougkusanagi/dev-lan',

    [ValidatePattern('^[A-Za-z0-9._/-]+$')]
    [string]$Ref = 'master',

    [ValidatePattern('^8\.(3|4|5)$')]
    [string]$PhpVersion = '8.5',

    [string]$InstallRoot = '',
    [string]$Distribution = '',
    [string]$SourceDir = '',
    [ValidateRange(0, 65535)]
    [int]$WindowsPort = 0,
    [switch]$SkipWSL,
    [switch]$SkipCaddy,
    [switch]$NoFirewall,
    [switch]$NoPath,
    [switch]$ConfirmWSLShutdown
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$script:TempWork = $null

# Windows PowerShell 5.1 needs an explicit UTF-8 output encoding for native
# commands. The file itself also carries a UTF-8 BOM so its literals are read
# correctly by both Windows PowerShell 5.1 and PowerShell 7+.
$utf8 = [System.Text.UTF8Encoding]::new($false)
[Console]::InputEncoding = $utf8
[Console]::OutputEncoding = $utf8
$OutputEncoding = $utf8

function Write-Step {
    param([string]$Message)
    Write-Host "`n==> $Message" -ForegroundColor Cyan
}

function Invoke-Native {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [string[]]$ArgumentList = @()
    )
    # Windows PowerShell 5.1 turns native stderr into ErrorRecord objects and,
    # with Stop enabled, may abort even when the process exits successfully.
    # Native process success is determined by its exit code instead.
    $previousErrorAction = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $output = & $FilePath @ArgumentList 2>&1
        $exitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorAction
    }
    if ($exitCode -ne 0) {
        $detail = (($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine).Trim()
        if ($detail.Length -gt 1200) {
            $detail = $detail.Substring(0, 1200)
        }
        throw "Comando falhou ($exitCode): $FilePath $($ArgumentList -join ' ')`n$detail"
    }
    $output
}

function Get-GitHubHeaders {
    $headers = @{ 'User-Agent' = 'DevLAN installer' }
    $token = $env:GH_TOKEN
    if (-not $token) {
        $token = $env:GITHUB_TOKEN
    }
    if ($token) {
        $headers['Authorization'] = "Bearer $token"
    }
    return $headers
}

function Get-Executable {
    param([Parameter(Mandatory = $true)][string]$Name)
    $command = Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $command) {
        return $null
    }
    return $command.Source
}

function Add-UserPath {
    param([Parameter(Mandatory = $true)][string]$Directory)
    if ($NoPath) {
        $env:Path = "$Directory;$env:Path"
        return
    }
    $resolved = [IO.Path]::GetFullPath($Directory).TrimEnd('\')
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @()
    if ($current) {
        $parts = @($current -split ';' | Where-Object { $_ -and $_.Trim() })
    }
    if (-not ($parts | Where-Object { $_.TrimEnd('\') -ieq $resolved })) {
        [Environment]::SetEnvironmentVariable('Path', (($parts + $resolved) -join ';'), 'User')
    }
    $env:Path = "$resolved;$env:Path"
}

function Require-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'Abra o PowerShell como Administrador para instalar WSL, Caddy e a regra de firewall.'
    }
}

function ConvertTo-WslPath {
    param([Parameter(Mandatory = $true)][string]$WindowsPath)
    $full = [IO.Path]::GetFullPath($WindowsPath).Replace('\', '/')
    if ($full -notmatch '^([A-Za-z]):/(.*)$') {
        throw "Caminho não está em um volume Windows convertível para WSL: $WindowsPath"
    }
    return "/mnt/$($Matches[1].ToLowerInvariant())/$($Matches[2])"
}

function Get-SourceRoot {
    if ($SourceDir) {
        $candidate = (Resolve-Path -LiteralPath $SourceDir -ErrorAction Stop).Path
        if (Test-Path -LiteralPath (Join-Path $candidate 'go.mod')) {
            return $candidate
        }
        throw "SourceDir não contém go.mod: $candidate"
    }

    $localCandidate = Split-Path -Parent $PSScriptRoot
    if ($localCandidate -and (Test-Path -LiteralPath (Join-Path $localCandidate 'go.mod'))) {
        return (Resolve-Path -LiteralPath $localCandidate).Path
    }

    $script:TempWork = Join-Path ([IO.Path]::GetTempPath()) ('devlan-install-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $script:TempWork -Force | Out-Null
    $archive = Join-Path $script:TempWork 'source.zip'
    $uri = "https://github.com/$Repository/archive/refs/heads/$Ref.zip"
    Write-Step "Baixando o código-fonte de $Repository ($Ref)"
    Invoke-WebRequest -Uri $uri -Headers (Get-GitHubHeaders) -OutFile $archive
    $extract = Join-Path $script:TempWork 'source'
    Expand-Archive -LiteralPath $archive -DestinationPath $extract -Force
    $module = Get-ChildItem -LiteralPath $extract -Filter 'go.mod' -File -Recurse | Select-Object -First 1
    if ($null -eq $module) {
        throw 'O arquivo baixado não contém um módulo Go válido.'
    }
    return $module.Directory.FullName
}

function Get-WslDistribution {
    if ($SkipWSL) {
        return $null
    }
    if (-not (Get-Executable 'wsl.exe')) {
        throw 'wsl.exe não está disponível. Instale o WSL pelo Windows antes de continuar.'
    }

    $installed = @(& wsl.exe --list --quiet 2>$null | ForEach-Object {
            ([string]$_).Replace([string][char]0, [string]'').Trim()
        } | Where-Object { $_ })
    if ($Distribution) {
        if ($installed -notcontains $Distribution) {
            throw "A distribuição WSL '$Distribution' não está instalada. Use wsl.exe --install -d $Distribution."
        }
        return $Distribution
    }
    if ($installed.Count -gt 0) {
        $ubuntu = $installed | Where-Object { $_ -match '^Ubuntu' } | Select-Object -First 1
        if ($ubuntu) {
            return $ubuntu
        }
        return $installed[0]
    }

    Write-Step 'Instalando WSL/Ubuntu'
    Invoke-Native 'wsl.exe' @('--install', '-d', 'Ubuntu') | Write-Host
    throw 'O WSL foi solicitado. Reinicie o Windows, conclua o primeiro usuário do Ubuntu e execute o instalador novamente.'
}

function Install-Go {
    Write-Step 'Verificando Go'
    $systemGo = Get-Executable 'go.exe'
    if ($systemGo) {
        & $systemGo version | Write-Host
        return $systemGo
    }

    $toolchainRoot = Join-Path $InstallRoot 'toolchains'
    $goRoot = Join-Path $toolchainRoot 'go'
    $goExe = Join-Path $goRoot 'bin\go.exe'
    if (-not (Test-Path -LiteralPath $goExe)) {
        $catalog = Invoke-RestMethod -Uri 'https://go.dev/dl/?mode=json'
        $release = @($catalog | Where-Object { $_.stable -eq $true }) | Select-Object -First 1
        $asset = @($release.files | Where-Object {
                $_.os -eq 'windows' -and $_.arch -eq 'amd64' -and $_.kind -eq 'archive'
            }) | Select-Object -First 1
        if ($null -eq $asset) {
            throw 'Não foi possível localizar um arquivo Go estável para Windows amd64.'
        }
        $archive = Join-Path $script:TempWork $asset.filename
        Invoke-WebRequest -Uri "https://go.dev/dl/$($asset.filename)" -OutFile $archive
        $actualHash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($asset.sha256 -and $actualHash -ne $asset.sha256.ToLowerInvariant()) {
            throw "Checksum inválido para o Go: esperado $($asset.sha256), obtido $actualHash"
        }
        New-Item -ItemType Directory -Path $toolchainRoot -Force | Out-Null
        if (Test-Path -LiteralPath $goRoot) {
            Remove-Item -LiteralPath $goRoot -Recurse -Force
        }
        Expand-Archive -LiteralPath $archive -DestinationPath $toolchainRoot -Force
        # This marker is the provenance proof used by uninstall. A Go
        # installation already present under the user's chosen root is not
        # removed unless this installer created it.
        New-Item -ItemType File -LiteralPath (Join-Path $goRoot '.devlan-managed') -Force | Out-Null
    }
    Add-UserPath (Split-Path -Parent $goExe)
    & $goExe version | Write-Host
    return $goExe
}

function Install-WslDependencies {
    param([Parameter(Mandatory = $true)][string]$WslDistribution, [Parameter(Mandatory = $true)][string]$WslScript)
    Write-Step "Instalando PHP $PhpVersion, PHP-FPM, Composer e Caddy no WSL ($WslDistribution)"
    $scriptPath = Join-Path $script:TempWork 'install-wsl.sh'
    $scriptText = [IO.File]::ReadAllText($WslScript).Replace("`r`n", "`n")
    [IO.File]::WriteAllText($scriptPath, $scriptText, [System.Text.UTF8Encoding]::new($false))
    $wslPath = ConvertTo-WslPath $scriptPath
    $caddyFlag = if ($SkipCaddy) { '0' } else { '1' }
    # Provision through WSL's root user. Calling sudo from a captured native
    # process can hide its password prompt and eventually fail with a timeout.
    Invoke-Native 'wsl.exe' @('--distribution', $WslDistribution, '--user', 'root', '--exec', 'bash', $wslPath, $PhpVersion, $caddyFlag) | Write-Host
}

function Build-Devlan {
    param([Parameter(Mandatory = $true)][string]$GoPath, [Parameter(Mandatory = $true)][string]$SourceRoot)
    Write-Step 'Compilando DevLAN'
    $bin = Join-Path $InstallRoot 'bin'
    New-Item -ItemType Directory -Path $bin -Force | Out-Null
    $target = Join-Path $bin 'devlan.exe'
    $resourcePath = Join-Path $SourceRoot 'cmd\devlan\devlan_resource_windows_amd64.syso'
    Push-Location $SourceRoot
    try {
        Invoke-Native $GoPath @('run', './cmd/devlan-resource', '-output', $resourcePath) | Write-Host
        # The installed executable is first and foremost a CLI. Keep the
        # console subsystem so `devlan`, `devlan -h` and errors are visible in
        # PowerShell. The desktop build tag still enables `devlan gui`.
        Invoke-Native $GoPath @('build', '-tags', 'desktop,production', '-ldflags', '-w -s', '-trimpath', '-o', $target, './cmd/devlan') | Write-Host
    } finally {
        if (Test-Path -LiteralPath $resourcePath) {
            Remove-Item -LiteralPath $resourcePath -Force
        }
        Pop-Location
    }
    Add-UserPath $bin
    return $target
}

function Build-DevlanWsl {
    param([Parameter(Mandatory = $true)][string]$GoPath, [Parameter(Mandatory = $true)][string]$SourceRoot)
    Write-Step 'Compilando cliente Linux do DevLAN para o WSL'
    $target = Join-Path $script:TempWork 'devlan-linux-amd64'
    Push-Location $SourceRoot
    $oldGoos = $env:GOOS
    $oldGoarch = $env:GOARCH
    $oldCgo = $env:CGO_ENABLED
    try {
        $env:GOOS = 'linux'
        $env:GOARCH = 'amd64'
        $env:CGO_ENABLED = '0'
        Invoke-Native $GoPath @('build', '-trimpath', '-ldflags', '-s -w', '-o', $target, './cmd/devlan') | Write-Host
    } finally {
        if ($null -eq $oldGoos) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $oldGoos }
        if ($null -eq $oldGoarch) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $oldGoarch }
        if ($null -eq $oldCgo) { Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue } else { $env:CGO_ENABLED = $oldCgo }
        Pop-Location
    }
    return $target
}

function Install-WslClient {
    param(
        [Parameter(Mandatory = $true)][string]$WslDistribution,
        [Parameter(Mandatory = $true)][string]$LinuxBinary,
        [Parameter(Mandatory = $true)][string]$WindowsDataDir
    )
    Write-Step "Instalando cliente devlan no WSL ($WslDistribution)"
    $binaryPath = ConvertTo-WslPath $LinuxBinary
    $dataFile = Join-Path $script:TempWork 'windows-data-dir'
    [IO.File]::WriteAllText($dataFile, (ConvertTo-WslPath $WindowsDataDir) + "`n", [System.Text.UTF8Encoding]::new($false))
    $dataPath = ConvertTo-WslPath $dataFile
    Invoke-Native 'wsl.exe' @('--distribution', $WslDistribution, '--user', 'root', '--exec', '/bin/mkdir', '-p', '/etc/devlan') | Write-Host
    Invoke-Native 'wsl.exe' @('--distribution', $WslDistribution, '--user', 'root', '--exec', '/bin/cp', $binaryPath, '/usr/local/bin/devlan') | Write-Host
    Invoke-Native 'wsl.exe' @('--distribution', $WslDistribution, '--user', 'root', '--exec', '/bin/chmod', '0755', '/usr/local/bin/devlan') | Write-Host
    Invoke-Native 'wsl.exe' @('--distribution', $WslDistribution, '--user', 'root', '--exec', '/bin/cp', $dataPath, '/etc/devlan/windows-data-dir') | Write-Host
    Invoke-Native 'wsl.exe' @('--distribution', $WslDistribution, '--user', 'root', '--exec', '/bin/chmod', '0644', '/etc/devlan/windows-data-dir') | Write-Host
}

try {
    if (-not $InstallRoot) {
        $InstallRoot = Join-Path $env:LOCALAPPDATA 'DevLAN'
    }
    $InstallRoot = [IO.Path]::GetFullPath($InstallRoot)
    New-Item -ItemType Directory -Path $InstallRoot -Force | Out-Null
    if (-not $SkipWSL -or -not $SkipCaddy -or -not $NoFirewall) {
        Require-Administrator
    }
    $sourceRoot = Get-SourceRoot
    if (-not $script:TempWork) {
        $script:TempWork = Join-Path ([IO.Path]::GetTempPath()) ('devlan-install-' + [Guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $script:TempWork -Force | Out-Null
    }

    $goPath = Install-Go
    $distribution = Get-WslDistribution
    if ($distribution) {
        Install-WslDependencies $distribution (Join-Path $sourceRoot 'scripts\install-wsl.sh')
    }

    $dataDir = $InstallRoot
    $devlanPath = Build-Devlan $goPath $sourceRoot
    $wslClientPath = $null
    if ($distribution) {
        $wslClientPath = Build-DevlanWsl $goPath $sourceRoot
        Install-WslClient $distribution $wslClientPath $dataDir
        Set-Content -LiteralPath (Join-Path $dataDir 'wsl-distribution') -Value $distribution -Encoding utf8
    }
    if ($WindowsPort -gt 0) {
        Write-Warning '-WindowsPort é mantido apenas por compatibilidade; a borda M8 usa 80/443 e as portas LAN atribuídas no WSL.'
    }
    $installArgs = @('--data-dir', $dataDir, 'install')
    if ($NoFirewall) {
        $installArgs += '--no-firewall'
    }
    Invoke-Native $devlanPath $installArgs | Write-Host

    if ($distribution -and -not $SkipCaddy) {
        Invoke-Native $devlanPath @('--data-dir', $dataDir, 'topology', 'repair') | Write-Host
        if ($ConfirmWSLShutdown) {
            Write-Warning 'A migração vai encerrar todas as distribuições WSL em execução.'
            Invoke-Native $devlanPath @('--data-dir', $dataDir, 'topology', 'migrate', '--yes') | Write-Host
        } else {
            Write-Host 'networkingMode=mirrored foi preparado. Execute `devlan topology migrate --yes` após salvar o trabalho em todas as distribuições.' -ForegroundColor Yellow
        }
    }

    Write-Host "`nDevLAN instalado em $InstallRoot" -ForegroundColor Green
    Write-Host 'Abra um novo terminal para que o PATH atualizado seja carregado.'
    Write-Host "Execute: devlan doctor"
} finally {
    if ($script:TempWork -and (Test-Path -LiteralPath $script:TempWork)) {
        Remove-Item -LiteralPath $script:TempWork -Recurse -Force -ErrorAction SilentlyContinue
    }
}
