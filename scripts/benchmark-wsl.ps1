param(
    [string]$Distribution = "",
    [ValidateRange(1, 20)]
    [int]$Rounds = 3,
    [ValidateRange(1, 128)]
    [int]$Items = 16
)

$wslTrueArgs = @("--exec", "/bin/true")
if ($Distribution.Trim() -ne "") {
    $wslTrueArgs = @("--distribution", $Distribution, "--exec", "/bin/true")
}

$loopValues = (1..$Items) -join " "
$batchScript = "for i in $loopValues; do /bin/true; done"
$wslBatchArgs = @("--exec", "/bin/sh", "-c", $batchScript)
if ($Distribution.Trim() -ne "") {
    $wslBatchArgs = @("--distribution", $Distribution, "--exec", "/bin/sh", "-c", $batchScript)
}

$directTotalMs = 0.0
$batchTotalMs = 0.0
for ($round = 0; $round -lt $Rounds; $round++) {
    $directTotalMs += (Measure-Command {
        for ($item = 0; $item -lt $Items; $item++) {
            & wsl.exe @wslTrueArgs | Out-Null
            if ($LASTEXITCODE -ne 0) {
                throw "wsl.exe falhou no benchmark direto (exit $LASTEXITCODE)"
            }
        }
    }).TotalMilliseconds

    $batchTotalMs += (Measure-Command {
        & wsl.exe @wslBatchArgs | Out-Null
        if ($LASTEXITCODE -ne 0) {
            throw "wsl.exe falhou no benchmark agrupado (exit $LASTEXITCODE)"
        }
    }).TotalMilliseconds
}

[pscustomobject]@{
    direct_spawns  = $Items
    batch_spawns   = 1
    direct_avg_ms  = [math]::Round($directTotalMs / $Rounds, 2)
    batch_avg_ms   = [math]::Round($batchTotalMs / $Rounds, 2)
    speedup        = [math]::Round($directTotalMs / $batchTotalMs, 2)
}
