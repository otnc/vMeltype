//go:build windows

package winime

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// script は、本家の meltype-ime.ps1 を読み込んで、DLL の確認と入れ替えだけを行う。
// パスは文字列に埋め込まず、環境変数で渡す。本家の Install-MeltypeIme は入力方式の一覧や既定の入力方式を変えることがあるので使わない。
const script = `
$ErrorActionPreference = 'Stop'
function Done([string]$result) {
    [IO.File]::WriteAllText($env:VMELTYPE_IME_RESULT, $result)
    exit 0
}
$imeScript = $env:VMELTYPE_IME_SCRIPT
if (-not (Test-Path -LiteralPath $imeScript)) { Done 'unsupported' }
. $imeScript
foreach ($name in 'Test-Arm64Windows', 'Get-MeltypeImeSourceHashes', 'Test-MeltypeImeCurrent', 'Invoke-Elevated') {
    if (-not (Get-Command $name -CommandType Function -ErrorAction SilentlyContinue)) { Done 'unsupported' }
}
$source = $env:VMELTYPE_IME_SOURCE
if (Test-Arm64Windows) { Done 'arm64' }
$hashes = Get-MeltypeImeSourceHashes $source
if (-not $hashes) { Done 'corrupt' }
if (Test-MeltypeImeCurrent $source) { Done 'current' }
switch (Invoke-Elevated $source $hashes 'Register') {
    'ok' { Done 'replaced' }
    'cancelled' { Done 'cancelled' }
    default { Done 'failed' }
}
`

// Replace は、appDir (切り替え先の版の、本家の ZIP の app に当たるディレクトリ) の meltype-ime.ps1 と tip を使って、
// 登録済みの IME の DLL を入れ替える。DLL が違うときだけ、本家が管理者権限の確認 (UAC) を出す。
// 呼び出し側は、Meltype IME が登録済みであることと、Meltype が止まっていることを確かめておく。
func Replace(ctx context.Context, appDir string) (Result, error) {
	powershell, err := windowsPowerShell()
	if err != nil {
		return "", err
	}
	resultFile, err := os.CreateTemp("", "vmeltype-ime-*.txt")
	if err != nil {
		return "", err
	}
	resultFile.Close()
	defer os.Remove(resultFile.Name())

	cmd := exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encode(script))
	cmd.Env = append(os.Environ(),
		"VMELTYPE_IME_SCRIPT="+filepath.Join(appDir, "meltype-ime.ps1"),
		"VMELTYPE_IME_SOURCE="+filepath.Join(appDir, "tip"),
		"VMELTYPE_IME_RESULT="+resultFile.Name(),
	)
	output, runErr := cmd.CombinedOutput()
	data, _ := os.ReadFile(resultFile.Name())
	result := Result(strings.TrimSpace(string(data)))
	if result == "" {
		return "", fmt.Errorf("Meltype IME の DLL を入れ替えられませんでした: %w\n%s", errors.Join(runErr, errors.New("結果が返りませんでした")), strings.TrimSpace(string(output)))
	}
	return result, nil
}

// windowsPowerShell は、PATH から探さず、Windows のフォルダーの powershell.exe を返す。本家のインストーラーと同じ。
func windowsPowerShell() (string, error) {
	dir, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		return "", fmt.Errorf("Windows のフォルダーを読めませんでした: %w", err)
	}
	return filepath.Join(dir, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), nil
}

// encode は、PowerShell の -EncodedCommand の形 (UTF-16LE の Base64) にする。
func encode(s string) string {
	units := utf16.Encode([]rune(s))
	b := make([]byte, len(units)*2)
	for i, u := range units {
		b[2*i] = byte(u)
		b[2*i+1] = byte(u >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}
