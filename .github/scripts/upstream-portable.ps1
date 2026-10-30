# 本家の portable 版を使って、vmeltype の取得、切り替え、snapshot、Meltype の終了と起動し直しを通しで確かめる。
# 本家の --exit は同じセッションで動いている Meltype に届き、普段使いの Meltype も止めてしまうので、
# GitHub Actions の使い捨ての Windows でだけ動かす。
param(
    [Parameter(Mandatory)] [string] $Vmeltype,
    [Parameter(Mandatory)] [string] $From,
    [Parameter(Mandatory)] [string] $To
)

$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true') {
    throw 'このスクリプトは、動いている Meltype を止めるので、GitHub Actions の中でだけ動かします'
}

$root = Join-Path $env:RUNNER_TEMP 'vmeltype-root'
$appDir = Join-Path $root 'portable\app'
$exe = Join-Path $appDir 'Meltype.exe'
$dataDir = Join-Path $appDir 'data'
$marker = Join-Path $dataDir 'vmeltype-check.txt'

function Invoke-Vmeltype {
    Write-Host "> vmeltype $args"
    $output = & $Vmeltype --root $root @args
    if ($LASTEXITCODE -ne 0) {
        throw "vmeltype $args が終了コード $LASTEXITCODE で失敗しました"
    }
    $text = $output -join "`n"
    Write-Host $text
    return $text
}

function Get-Running {
    @(Get-Process -Name Meltype -ErrorAction SilentlyContinue | Where-Object { $_.Path -like "$appDir\*" })
}

function Wait-Until([scriptblock] $Condition, [string] $Message, [int] $Seconds = 30) {
    $deadline = (Get-Date).AddSeconds($Seconds)
    while (-not (& $Condition)) {
        if ((Get-Date) -gt $deadline) {
            throw $Message
        }
        Start-Sleep -Milliseconds 200
    }
}

function Assert-Running([string] $Tag) {
    Wait-Until { (Get-Running).Count -gt 0 } "$Tag の Meltype が動いていません"
    $version = (Get-Item $exe).VersionInfo.ProductVersion
    Write-Host "動いている Meltype: $version"
    if (-not $version.StartsWith($Tag.TrimStart('v'))) {
        throw "実行先の Meltype の版が $version で、$Tag ではありません"
    }
}

function Assert-Marker([bool] $Expected) {
    if ((Test-Path $marker) -ne $Expected) {
        throw "data に置いた $marker が、あるはずなのに $(-not $Expected) の状態です"
    }
}

Invoke-Vmeltype install $From --portable -y | Out-Null
Invoke-Vmeltype install $To --portable -y | Out-Null
Invoke-Vmeltype list | Out-Null

Write-Host '--- 有効にして起動する'
Invoke-Vmeltype use $From --portable -y | Out-Null
Invoke-Vmeltype run | Out-Null
Assert-Running $From
# 動いている間に run をもう一度使っても、二つ目は起動しない
Invoke-Vmeltype run | Out-Null
if ((Get-Running).Count -ne 1) {
    throw "Meltype が $((Get-Running).Count) 個動いています"
}
# Meltype は起動すると data と設定のファイルを作る
Wait-Until { Test-Path (Join-Path $dataDir 'config.json') } 'Meltype が data に設定を作りません'
$before = (Invoke-Vmeltype snapshot create -y --json | ConvertFrom-Json).id
Assert-Running $From
Set-Content -Path $marker -Value 'vMeltype の検査で置いたファイル'

Write-Host '--- 動いている Meltype を終了させて切り替える'
Invoke-Vmeltype use $To --portable -y | Out-Null
Assert-Running $To
Assert-Marker $true
$current = Invoke-Vmeltype current --json | ConvertFrom-Json
if ($current.active.tag.TrimStart('v') -ne $To.TrimStart('v')) {
    throw "有効版の記録が $($current.active.tag) です"
}

Write-Host '--- rollback で戻す'
Invoke-Vmeltype rollback -y | Out-Null
Assert-Running $From
Assert-Marker $true

Write-Host '--- 動いている Meltype の状態を調べる'
# Meltype が動いて data に書いても、実行先の配置は保持版と一致したままになる
$status = Invoke-Vmeltype status --json | ConvertFrom-Json
if ($status.deployment -ne 'match' -or $status.running.Count -eq 0 -or -not $status.dataExists) {
    throw "status が違います: deployment = $($status.deployment)、running = $($status.running.Count)、dataExists = $($status.dataExists)"
}
Invoke-Vmeltype doctor | Out-Null

Write-Host '--- snapshot を復元する'
Invoke-Vmeltype snapshot restore $before -y | Out-Null
Assert-Running $From
Assert-Marker $false
$snapshots = (Invoke-Vmeltype snapshot list --json | ConvertFrom-Json).snapshots
if ($snapshots.Count -ne 2) {
    throw "復元の前の data を保存していません ($($snapshots.Count) 件)"
}

Write-Host '--- 後片付け'
# 起動し直した直後の Meltype は合図の受け口がまだなく、--exit が 1 で終わるので、届くまで送り直す
Wait-Until {
    (Start-Process -FilePath $exe -ArgumentList '--exit' -Wait -PassThru).ExitCode -eq 0
} 'Meltype に終了の合図を送れません'
Wait-Until { (Get-Running).Count -eq 0 } 'Meltype が終わりません'
Write-Host '通しの検査が終わりました'
