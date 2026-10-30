# 本家の通常版を Install.cmd と同じ install.ps1 で入れた環境を、vmeltype が取り込んで切り替えることを通しで確かめる。
# 利用者の %LOCALAPPDATA% の Meltype と自動起動を書き換え、動いている Meltype も止めるので、GitHub Actions の使い捨ての Windows でだけ動かす。
# -Ime を付けると、install.ps1 -Ask で Meltype IME も登録し (GitHub Actions は管理者で動き、UAC の確認は出ない)、切り替えで DLL も入れ替わることを確かめる。
# 付けなければ IME は登録しない (install.ps1 は -Ask を付けなければ登録を求めない)。
param(
    [Parameter(Mandatory)] [string] $Vmeltype,
    [Parameter(Mandatory)] [string] $From,
    [Parameter(Mandatory)] [string] $To,
    [switch] $Ime
)

$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true') {
    throw 'このスクリプトは、利用者の Meltype を書き換えるので、GitHub Actions の中でだけ動かします'
}

$root = Join-Path $env:RUNNER_TEMP 'vmeltype-root'
$appDir = Join-Path $env:LOCALAPPDATA 'Programs\Meltype'
$exe = Join-Path $appDir 'Meltype.exe'
$dataDir = Join-Path $env:LOCALAPPDATA 'Meltype'
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

function Assert-AutoUpdateDisabled {
    $config = Get-Content -LiteralPath (Join-Path $dataDir 'config.json') -Raw | ConvertFrom-Json
    if ($config.AutoUpdate -ne $false) {
        throw "本家の自動更新が止まっていません: $($config.AutoUpdate)"
    }
}

$tipKey = 'HKLM:\SOFTWARE\Microsoft\CTF\TIP\{417D801B-A9BD-4C26-BD16-356A825A6998}'
if (Test-Path $tipKey) {
    throw 'この環境には Meltype IME が既に登録されています。登録していない環境から始めます'
}

function Get-StoredPackage([string] $Tag) {
    $stored = (Invoke-Vmeltype list --json | ConvertFrom-Json).stored | Where-Object { $_.tag.TrimStart('v') -eq $Tag.TrimStart('v') -and $_.variant -eq 'standard' }
    return Join-Path $root "versions\$($stored.id)\files"
}

# 登録した IME の DLL が、Tag の版のものかを、本家が DLL の隣に置く署名する前の中身のハッシュで確かめる
function Assert-ImeDll([string] $Tag) {
    if (-not $Ime) { return }
    $programFiles = [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::LocalMachine, [Microsoft.Win32.RegistryView]::Registry64).OpenSubKey('SOFTWARE\Microsoft\Windows\CurrentVersion').GetValue('ProgramFilesDir')
    foreach ($arch in 'x64', 'x86') {
        $installed = (Get-Content -LiteralPath (Join-Path $programFiles "Meltype\tip\$arch\MeltypeTip.dll.sha256") -Raw).Trim()
        $expected = (Get-Content -LiteralPath (Join-Path (Get-StoredPackage $Tag) "app\tip\$arch\MeltypeTip.dll.sha256") -Raw).Trim()
        Write-Host "IME の DLL ($arch): $installed"
        if ($installed -ne $expected) {
            throw "登録した IME の DLL ($arch) が $Tag のものではありません"
        }
    }
}

Invoke-Vmeltype install $From -y | Out-Null
Invoke-Vmeltype install $To -y | Out-Null

Write-Host '--- 本家の install.ps1 で入れて起動する'
$installArgs = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', (Join-Path (Get-StoredPackage $From) 'install.ps1'))
if ($Ime) { $installArgs += '-Ask' }
& powershell.exe @installArgs
if ($LASTEXITCODE -ne 0) {
    throw "本家の install.ps1 が終了コード $LASTEXITCODE で失敗しました"
}
if ($Ime -ne (Test-Path $tipKey)) {
    throw "Meltype IME の登録が、-Ime の指定 ($Ime) と合いません"
}
Assert-Running $From
Assert-ImeDll $From
Wait-Until { Test-Path (Join-Path $dataDir 'config.json') } 'Meltype がデータ先に設定を作りません'
Set-Content -Path $marker -Value 'vMeltype の検査で置いたファイル'
$uninstallKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Meltype'
if (-not (Test-Path $uninstallKey)) {
    throw '本家の install.ps1 が「設定 → アプリ」に登録していません'
}

Write-Host '--- 本家のインストールを取り込んで切り替える'
Invoke-Vmeltype use $To -y | Out-Null
Assert-Running $To
Assert-ImeDll $To
Assert-AutoUpdateDisabled
if (-not (Test-Path $marker)) {
    throw 'データ先に置いたファイルが消えました'
}
if (-not (Test-Path (Join-Path $appDir 'uninstall.ps1'))) {
    throw '本家のアンインストール用のファイルを引き継いでいません'
}
if (-not (Test-Path (Join-Path $root 'baseline\standard-app\Meltype.exe'))) {
    throw '置き換えた本家のファイルを管理前の記録に残していません'
}
$snapshots = (Invoke-Vmeltype snapshot list --json | ConvertFrom-Json).snapshots
if (@($snapshots | Where-Object reason -eq 'before-use').Count -ne 1) {
    throw '切り替えの前のスナップショットがありません'
}

Write-Host '--- 動いている Meltype の状態を調べる'
# 本家が起動して設定を保存し直しても、自動更新は止まったままになる
Assert-AutoUpdateDisabled
$status = Invoke-Vmeltype status --json | ConvertFrom-Json
if ($status.deployment -ne 'match' -or $status.running.Count -eq 0 -or $status.dataDir -ne $dataDir) {
    throw "status が違います: deployment = $($status.deployment)、running = $($status.running.Count)、dataDir = $($status.dataDir)"
}
Invoke-Vmeltype doctor | Out-Null

Write-Host '--- 元の版に切り替える'
Invoke-Vmeltype use $From -y | Out-Null
Assert-Running $From
Assert-ImeDll $From
Assert-AutoUpdateDisabled
if (-not (Test-Path $marker)) {
    throw 'データ先に置いたファイルが消えました'
}

Write-Host '--- Meltype を残して vMeltype をやめる'
Invoke-Vmeltype app uninstall -y | Out-Null
if (Test-Path $root) {
    throw '管理ルートが残っています'
}
$config = Get-Content -LiteralPath (Join-Path $dataDir 'config.json') -Raw | ConvertFrom-Json
if ($config.AutoUpdate -ne $true) {
    throw "本家の自動更新を戻していません: $($config.AutoUpdate)"
}
Assert-Running $From
if (-not (Test-Path $marker)) {
    throw 'データ先に置いたファイルが消えました'
}

Write-Host '--- もう一度取り込み、Meltype も削除して vMeltype をやめる'
Invoke-Vmeltype install $To -y | Out-Null
Invoke-Vmeltype use $To -y | Out-Null
Assert-Running $To
Invoke-Vmeltype app uninstall --remove-meltype -y | Out-Null
foreach ($path in $root, $appDir, $uninstallKey) {
    if (Test-Path $path) {
        throw "$path が残っています"
    }
}
if ($Ime -and (Test-Path $tipKey)) {
    throw 'Meltype IME の登録が残っています'
}
if ((Get-Running).Count -ne 0) {
    throw 'Meltype が動いています'
}
Write-Host '通しの検査が終わりました'
