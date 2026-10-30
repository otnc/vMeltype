//go:build windows

package upstreamremove

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// Run は、appDir (本家のインストール先) の Meltype を、本家のアンインストーラーで削除し、Meltype.exe が消えるまで最長 timeout だけ待つ。
// unins000.exe があればそれを、なければ appDir の uninstall.ps1 を、それもなければ fallbackScript を使う。
// interactive なら unins000.exe を画面ありで動かし、確認と、設定と学習データを消すかを本家に聞かせる。そうでなければ画面なしで動かす (本家は、このときデータを残す)。
// 呼び出し側は、利用者が Meltype の削除を明示したことと、Meltype が止まっていることを確かめておく。
func Run(ctx context.Context, appDir, fallbackScript string, interactive bool, timeout time.Duration) (Method, error) {
	var method Method
	var cmd *exec.Cmd
	inno := filepath.Join(appDir, "unins000.exe")
	script := filepath.Join(appDir, "uninstall.ps1")
	switch {
	case exists(inno):
		method = MethodInno
		args := []string{}
		if !interactive {
			args = append(args, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART")
		}
		cmd = exec.CommandContext(ctx, inno, args...)
	case exists(script):
		method = MethodScript
		ps, err := powershell()
		if err != nil {
			return "", err
		}
		cmd = exec.CommandContext(ctx, ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script)
	case fallbackScript != "" && exists(fallbackScript):
		method = MethodFallbackScript
		ps, err := powershell()
		if err != nil {
			return "", err
		}
		cmd = exec.CommandContext(ctx, ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", fallbackScript)
	default:
		return "", fmt.Errorf("%s に本家のアンインストーラーが見つからないので、Meltype を削除できません", appDir)
	}
	// アンインストーラーがインストール先を消せるよう、インストール先の外で動かす
	cmd.Dir = os.TempDir()
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return method, fmt.Errorf("本家のアンインストーラーが失敗しました: %w", err)
	}

	// unins000.exe は自分を一時フォルダーに写して、そちらで削除を続けるので、終わるのを Meltype.exe が消えたことで確かめる
	exe := filepath.Join(appDir, "Meltype.exe")
	deadline := time.Now().Add(timeout)
	for exists(exe) {
		if time.Now().After(deadline) {
			return method, fmt.Errorf("本家のアンインストーラーが終わっても、%s が残っています。Meltype は削除できていません", exe)
		}
		select {
		case <-ctx.Done():
			return method, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return method, nil
}

func powershell() (string, error) {
	dir, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		return "", fmt.Errorf("Windows のフォルダーを読めませんでした: %w", err)
	}
	return filepath.Join(dir, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}
