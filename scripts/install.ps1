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
    [switch]$NoPath
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

function Test-TcpPortAvailable {
    param([Parameter(Mandatory = $true)][int]$Port)
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Any, $Port)
    try {
        $listener.Start()
        return $true
    } catch {
        return $false
    } finally {
        $listener.Stop()
    }
}

function Test-TcpEndpoint {
    param([Parameter(Mandatory = $true)][string]$Address, [Parameter(Mandatory = $true)][int]$Port)
    $client = [Net.Sockets.TcpClient]::new()
    try {
        return $client.ConnectAsync($Address, $Port).Wait(500)
    } catch {
        return $false
    } finally {
        $client.Dispose()
    }
}

function Select-WindowsPort {
    if ($WindowsPort -gt 0) {
        return $WindowsPort
    }
    $configured = 80
    $configPath = Join-Path $InstallRoot 'config.toml'
    if (Test-Path -LiteralPath $configPath) {
        $line = Get-Content -LiteralPath $configPath | Where-Object { $_ -match '^windows_port\s*=\s*\d+' } | Select-Object -First 1
        if ($line -and $line -match '(\d+)') {
            $configured = [int]$Matches[1]
        }
    }
    if ((Test-TcpPortAvailable $configured) -or (Test-TcpEndpoint '127.0.0.1' 2019)) {
        return $configured
    }
    foreach ($candidate in @(8080, 8081, 8888)) {
        if (Test-TcpPortAvailable $candidate) {
            Write-Warning "A porta $configured já está ocupada; o DevLAN usará a porta $candidate."
            return $candidate
        }
    }
    throw 'Nenhuma porta HTTP disponível entre 80, 8080, 8081 e 8888.'
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
    }
    Add-UserPath (Split-Path -Parent $goExe)
    & $goExe version | Write-Host
    return $goExe
}

function Find-InstalledCaddy {
    $command = Get-Executable 'caddy.exe'
    if ($command) {
        return $command
    }
    $roots = @(
        (Join-Path $InstallRoot 'bin'),
        (Join-Path $env:LOCALAPPDATA 'Microsoft\WinGet\Packages'),
        (Join-Path $env:ProgramFiles 'Caddy')
    ) | Where-Object { $_ -and (Test-Path -LiteralPath $_) }
    foreach ($root in $roots) {
        $found = Get-ChildItem -LiteralPath $root -Filter 'caddy.exe' -File -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($found) {
            return $found.FullName
        }
    }
    return $null
}

function Install-WindowsCaddy {
    if ($SkipCaddy) {
        return $null
    }
    Write-Step 'Instalando Caddy no Windows'
    $existing = Find-InstalledCaddy
    if ($existing) {
        Add-UserPath (Split-Path -Parent $existing)
        return $existing
    }

    $winget = Get-Executable 'winget.exe'
    if ($winget) {
        try {
            Invoke-Native $winget @(
                'install', '--id', 'CaddyServer.Caddy', '--exact',
                '--accept-source-agreements', '--accept-package-agreements', '--silent'
            ) | Write-Host
        } catch {
            Write-Warning "winget não instalou Caddy; tentando o binário oficial: $($_.Exception.Message)"
        }
    }
    $existing = Find-InstalledCaddy
    if (-not $existing) {
        $headers = Get-GitHubHeaders
        $headers['Accept'] = 'application/vnd.github+json'
        $release = Invoke-RestMethod -Uri 'https://api.github.com/repos/caddyserver/caddy/releases/latest' -Headers $headers
        $asset = @($release.assets | Where-Object { $_.name -match 'windows_amd64\.zip$' }) | Select-Object -First 1
        $checksums = @($release.assets | Where-Object { $_.name -match 'checksums' }) | Select-Object -First 1
        if ($null -eq $asset -or $null -eq $checksums) {
            throw 'A release oficial do Caddy não contém o binário ou checksums esperados.'
        }
        $zip = Join-Path $script:TempWork $asset.name
        $checksumFile = Join-Path $script:TempWork $checksums.name
        Invoke-WebRequest -Uri $asset.browser_download_url -Headers $headers -OutFile $zip
        Invoke-WebRequest -Uri $checksums.browser_download_url -Headers $headers -OutFile $checksumFile
        $line = Get-Content -LiteralPath $checksumFile | Where-Object { $_ -match "\s$([Regex]::Escape($asset.name))$" } | Select-Object -First 1
        if (-not $line) {
            throw "Checksum não encontrado para $($asset.name)."
        }
        if ($line -notmatch '^([0-9a-fA-F]{64})\s+') {
            throw "Checksum inválido para $($asset.name)."
        }
        $expected = $Matches[1].ToLowerInvariant()
        $actual = (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($expected -ne $actual) {
            throw "Checksum inválido para o Caddy: esperado $expected, obtido $actual"
        }
        $bin = Join-Path $InstallRoot 'bin'
        New-Item -ItemType Directory -Path $bin -Force | Out-Null
        Expand-Archive -LiteralPath $zip -DestinationPath $bin -Force
        $existing = Join-Path $bin 'caddy.exe'
    }
    if (-not (Test-Path -LiteralPath $existing)) {
        throw 'Caddy não foi encontrado após a instalação.'
    }
    Add-UserPath (Split-Path -Parent $existing)
    & $existing version | Select-Object -First 1 | Write-Host
    return $existing
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

function Sync-WslCaddy {
    param([Parameter(Mandatory = $true)][string]$WslDistribution, [Parameter(Mandatory = $true)][string]$ConfigPath)
    Write-Step "Sincronizando Caddy no WSL ($WslDistribution)"
    $wslConfig = ConvertTo-WslPath $ConfigPath
    Invoke-Native 'wsl.exe' @('--distribution', $WslDistribution, '--user', 'root', '--exec', '/bin/mkdir', '-p', '/etc/caddy') | Write-Host
    Invoke-Native 'wsl.exe' @('--distribution', $WslDistribution, '--user', 'root', '--exec', '/bin/cp', $wslConfig, '/etc/caddy/Caddyfile') | Write-Host
    try {
        Invoke-Native 'wsl.exe' @('--distribution', $WslDistribution, '--user', 'root', '--exec', '/usr/bin/systemctl', 'restart', 'caddy') | Write-Host
    } catch {
        Invoke-Native 'wsl.exe' @('--distribution', $WslDistribution, '--user', 'root', '--exec', '/usr/sbin/service', 'caddy', 'restart') | Write-Host
    }
}

function Start-WindowsCaddy {
    param([Parameter(Mandatory = $true)][string]$CaddyPath, [Parameter(Mandatory = $true)][string]$ConfigPath)
    Write-Step 'Iniciando ou recarregando Caddy no Windows'
    try {
        Invoke-Native $CaddyPath @('reload', '--address', '127.0.0.1:2019', '--config', $ConfigPath, '--adapter', 'caddyfile') | Write-Host
    } catch {
        # `caddy start` launches a background child that inherits stdout/stderr.
        # Capturing those pipes makes Windows PowerShell wait forever for EOF.
        $previousErrorAction = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'Continue'
            & $CaddyPath start --config $ConfigPath --adapter caddyfile
            $exitCode = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $previousErrorAction
        }
        if ($exitCode -ne 0) {
            throw "Caddy não iniciou (código $exitCode). Verifique se a porta HTTP configurada está disponível."
        }
    }
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
        # Wails requires the desktop build tag. Production enables the
        # production runtime path, and windowsgui prevents a console window
        # from being created when the GUI is launched.
        Invoke-Native $GoPath @('build', '-tags', 'desktop,production', '-ldflags', '-w -s -H windowsgui', '-trimpath', '-o', $target, './cmd/devlan') | Write-Host
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
    $caddyPath = Install-WindowsCaddy
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
    $selectedWindowsPort = Select-WindowsPort
    $installArgs = @('--data-dir', $dataDir, 'install', '--windows-port', [string]$selectedWindowsPort)
    if ($NoFirewall) {
        $installArgs += '--no-firewall'
    }
    Invoke-Native $devlanPath $installArgs | Write-Host

    if ($distribution -and -not $SkipCaddy) {
        Sync-WslCaddy $distribution (Join-Path $dataDir 'generated\Caddyfile.wsl')
    }
    if ($caddyPath) {
        Start-WindowsCaddy $caddyPath (Join-Path $dataDir 'generated\Caddyfile.windows')
    }
    if ($caddyPath -or $distribution) {
        Invoke-Native $devlanPath @('--data-dir', $dataDir, 'reload') | Write-Host
    }

    Write-Host "`nDevLAN instalado em $InstallRoot" -ForegroundColor Green
    Write-Host 'Abra um novo terminal para que o PATH atualizado seja carregado.'
    Write-Host "Execute: devlan doctor"
} finally {
    if ($script:TempWork -and (Test-Path -LiteralPath $script:TempWork)) {
        Remove-Item -LiteralPath $script:TempWork -Recurse -Force -ErrorAction SilentlyContinue
    }
}
