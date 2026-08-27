[CmdletBinding()]
param(
    [string]$DevlanPath = 'devlan.exe',
    [string]$DataDir = '',
    [string]$Distribution = '',
    [string[]]$ProbeUrl = @(),
    [ValidateRange(0, 65535)]
    [int]$OccupiedPort = 0,
    [switch]$AfterWSLShutdown,
    [switch]$RunMigration,
    [switch]$RequireCA,
    [switch]$RequireFirewall
)

$ErrorActionPreference = 'Stop'

function Invoke-DevlanText {
    param([Parameter(Mandatory = $true)][string[]]$Command)
    $arguments = @()
    if ($DataDir) {
        $arguments += @('--data-dir', $DataDir)
    }
    $arguments += $Command
    $output = & $DevlanPath @arguments 2>&1
    $exitCode = $LASTEXITCODE
    $text = (($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine).Trim()
    if ($exitCode -ne 0) {
        throw "DevLAN falhou ($exitCode): $($Command -join ' ')`n$text"
    }
    return $text
}

function Invoke-DevlanJson {
    param([Parameter(Mandatory = $true)][string[]]$Command)
    $text = Invoke-DevlanText $Command
    try {
        return $text | ConvertFrom-Json
    } catch {
        throw "DevLAN não retornou JSON para $($Command -join ' '): $($_.Exception.Message)`n$text"
    }
}

function Assert-Property {
    param(
        [Parameter(Mandatory = $true)]$Object,
        [Parameter(Mandatory = $true)][string]$Name
    )
    if ($null -eq $Object.PSObject.Properties[$Name]) {
        throw "campo obrigatório ausente no diagnóstico M8: $Name"
    }
}

function Assert-HealthyTopology {
    param([Parameter(Mandatory = $true)]$Report)
    foreach ($field in @('supported', 'wsl2', 'mirroredNetworking', 'systemd', 'loopbackBidirectional', 'lanReachable', 'portConflicts')) {
        Assert-Property $Report $field
    }
    if (-not $Report.supported -or -not $Report.wsl2 -or -not $Report.mirroredNetworking -or -not $Report.systemd -or -not $Report.loopbackBidirectional -or -not $Report.lanReachable) {
        throw "compatibilidade M8 não está saudável: $($Report | ConvertTo-Json -Compress)"
    }
    if (@($Report.portConflicts).Count -ne 0) {
        throw "há conflitos de porta: $($Report.portConflicts | ConvertTo-Json -Compress)"
    }
}

$wslCommand = @(Get-Command 'wsl.exe' -CommandType Application -ErrorAction SilentlyContinue) | Select-Object -First 1
if ($null -eq $wslCommand) {
    throw 'wsl.exe não encontrado'
}
$wslPath = $wslCommand.Path
& $wslPath --version | Out-Host

if (-not $Distribution) {
    $installed = @(& $wslPath --list --quiet 2>$null | ForEach-Object {
            ([string]$_).Replace([string][char]0, [string]'').Trim()
        } | Where-Object { $_ })
    if ($installed.Count -eq 0) {
        throw 'nenhuma distribuição WSL instalada'
    }
    $Distribution = $installed[0]
}
& $wslPath --distribution $Distribution --exec /bin/true
if ($LASTEXITCODE -ne 0) {
    throw "a distribuição WSL '$Distribution' não pôde executar /bin/true"
}

$report = Invoke-DevlanJson @('topology', 'check', '--json')
Assert-HealthyTopology $report
Write-Host "[OK] topology check: $Distribution / mirrored / systemd / loopback / LAN"

$status = Invoke-DevlanJson @('topology', 'status', '--json')
Assert-Property $status 'topology'
Assert-Property $status 'caddy'
if ($status.topology.topology -ne 'single-wsl') {
    throw "topologia esperada single-wsl, obtida $($status.topology.topology)"
}
if (-not $status.caddy.live) {
    throw 'o endpoint admin/config do Caddy WSL não está vivo'
}

if ($OccupiedPort -gt 0) {
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $OccupiedPort)
    try {
        $listener.Start()
        $occupied = Invoke-DevlanJson @('topology', 'check', '--json')
        $match = @($occupied.portConflicts | Where-Object { $_.port -eq $OccupiedPort })
        if ($match.Count -eq 0) {
            throw "porta ocupada $OccupiedPort não apareceu em portConflicts"
        }
        Write-Host "[OK] conflito de porta $OccupiedPort detectado"
    } finally {
        $listener.Stop()
    }
}

if ($AfterWSLShutdown) {
    Write-Warning 'wsl --shutdown encerra todas as distribuições em execução.'
    & $wslPath --shutdown
    if ($LASTEXITCODE -ne 0) {
        throw "wsl --shutdown falhou com código $LASTEXITCODE"
    }
    $afterShutdown = Invoke-DevlanJson @('topology', 'check', '--json')
    Assert-HealthyTopology $afterShutdown
    Write-Host '[OK] topology check após wsl --shutdown'
}

if ($RunMigration) {
    Invoke-DevlanText @('topology', 'migrate', '--yes') | Write-Host
    $afterMigration = Invoke-DevlanJson @('topology', 'status', '--json')
    if ($afterMigration.topology.topology -ne 'single-wsl') {
        throw 'a migração não terminou em single-wsl'
    }
    Write-Host '[OK] migração explícita e confirmação pós-migração'
}

if ($RequireCA) {
    $ca = Invoke-DevlanText @('ca', 'info')
    if ($ca -notmatch 'Certificado existente:\s*true') {
        throw "CA local ausente ou não diagnosticada como válida:`n$ca"
    }
    Write-Host '[OK] CA local presente'
}

if ($RequireFirewall) {
    $doctor = Invoke-DevlanText @('doctor')
    if ($doctor -notmatch 'Firewall' -or $doctor -notmatch 'Hyper-V') {
        throw "doctor não exibiu os dois componentes de firewall:`n$doctor"
    }
    Write-Host '[OK] Windows Firewall + Hyper-V Firewall diagnosticados'
}

if ($ProbeUrl.Count -gt 0) {
    $previousCallback = [Net.ServicePointManager]::ServerCertificateValidationCallback
    [Net.ServicePointManager]::ServerCertificateValidationCallback = { $true }
    try {
        foreach ($url in $ProbeUrl) {
            $response = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 10
            if ($response.StatusCode -ge 500) {
                throw "endpoint $url retornou HTTP $($response.StatusCode)"
            }
            Write-Host "[OK] $url -> HTTP $($response.StatusCode)"
        }
    } finally {
        [Net.ServicePointManager]::ServerCertificateValidationCallback = $previousCallback
    }
}

Write-Host 'Smoke M8 concluído. Execute novamente após reboot, VPN ou troca de IP para validar esses estados.' -ForegroundColor Green
