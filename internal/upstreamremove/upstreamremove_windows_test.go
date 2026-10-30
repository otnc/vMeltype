//go:build windows

package upstreamremove

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeInstall は、本家のインストール先と同じく Meltype.exe を置き、uninstall.ps1 の代わりに、指定したスクリプトを置く。
func fakeInstall(t *testing.T, script string) (appDir string) {
	t.Helper()
	appDir = filepath.Join(t.TempDir(), "Programs", "Meltype")
	os.MkdirAll(appDir, 0o700)
	os.WriteFile(filepath.Join(appDir, "Meltype.exe"), []byte("exe"), 0o600)
	if script != "" {
		os.WriteFile(filepath.Join(appDir, "uninstall.ps1"), []byte("\xEF\xBB\xBF"+script), 0o600)
	}
	return appDir
}

// removeSelf は、本家の uninstall.ps1 と同じく、自分のいるインストール先を消す。
const removeSelf = "Set-Location -LiteralPath $env:TEMP\nRemove-Item -LiteralPath $PSScriptRoot -Recurse -Force\n"

func TestRun(t *testing.T) {
	t.Run("インストール先の uninstall.ps1 で消す", func(t *testing.T) {
		appDir := fakeInstall(t, removeSelf)

		method, err := Run(context.Background(), appDir, "", false, time.Minute)

		if err != nil || method != MethodScript {
			t.Fatalf("method = %q, err = %v", method, err)
		}
		if _, err := os.Stat(appDir); !os.IsNotExist(err) {
			t.Errorf("インストール先が残っている")
		}
	})
	t.Run("インストール先になければ、渡された uninstall.ps1 で消す", func(t *testing.T) {
		appDir := fakeInstall(t, "")
		fallback := filepath.Join(t.TempDir(), "uninstall.ps1")
		os.WriteFile(fallback, []byte("\xEF\xBB\xBFRemove-Item -LiteralPath $env:MELTOOL_TEST_APP -Recurse -Force\n"), 0o600)
		t.Setenv("MELTOOL_TEST_APP", appDir)

		method, err := Run(context.Background(), appDir, fallback, false, time.Minute)

		if err != nil || method != MethodFallbackScript {
			t.Fatalf("method = %q, err = %v", method, err)
		}
	})
	t.Run("アンインストーラーが終わっても Meltype.exe が残っていれば失敗にする", func(t *testing.T) {
		appDir := fakeInstall(t, "exit 0\n")

		_, err := Run(context.Background(), appDir, "", false, 2*time.Second)

		if err == nil {
			t.Error("消えていないのに成功にした")
		}
	})
	t.Run("アンインストーラーがなければ何もしない", func(t *testing.T) {
		appDir := fakeInstall(t, "")

		_, err := Run(context.Background(), appDir, "", false, time.Minute)

		if err == nil {
			t.Error("アンインストーラーがないのに成功にした")
		}
		if _, err := os.Stat(filepath.Join(appDir, "Meltype.exe")); err != nil {
			t.Errorf("インストール先を変えた")
		}
	})
}
