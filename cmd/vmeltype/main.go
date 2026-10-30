// vmeltype は、Meltype の複数の版を保持し、切り替える CLI。
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"time"

	"golang.org/x/term"

	"github.com/otnc/vmeltype/internal/appctl"
	"github.com/otnc/vmeltype/internal/assets"
	"github.com/otnc/vmeltype/internal/catalog/remote"
	"github.com/otnc/vmeltype/internal/cli"
	"github.com/otnc/vmeltype/internal/osproc"
	"github.com/otnc/vmeltype/internal/rootdir"
	"github.com/otnc/vmeltype/internal/winime"
	"github.com/otnc/vmeltype/internal/winsys"
)

// version は、配布のビルドで -ldflags "-X main.version=..." で埋め込む。
var version = "dev"

// stopAppTimeout は、Meltype に終了を求めてから終わるまで待つ時間。本家は合図のあと学習データを書いてから終わる。
const stopAppTimeout = 15 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	env := cli.Env{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
		// 確認は標準エラーに出すので、標準出力ではなく標準エラーが TTY かを見る
		Interactive: term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd())),
		Version:     version,
		Now:         time.Now,
		Getenv:      os.Getenv,
		DefaultRoot: func() (string, error) {
			home, err := os.UserHomeDir()
			if err != nil && runtime.GOOS != "windows" {
				return "", err
			}
			return rootdir.Default(runtime.GOOS, os.Getenv, home)
		},
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
		GitHubAPIURL: remote.DefaultBaseURL,
		Platform:     assets.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
		RunningUnder: osproc.RunningUnder,
		StopApp: func(appDir string) error {
			return appctl.Stop(ctx, appDir, osproc.RunningUnder, stopAppTimeout)
		},
		StartApp:      appctl.Start,
		LocalAppData:  winsys.LocalAppData,
		IMERegistered: winsys.IMERegistered,
		ReplaceIME: func(appDir string) (winime.Result, error) {
			return winime.Replace(ctx, appDir)
		},
	}
	code := cli.RunContext(ctx, os.Args[1:], env)
	stop()
	os.Exit(code)
}
