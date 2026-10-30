// vmeltype は、Meltype の複数のバージョンを保持し、切り替える CLI。
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/mattn/go-colorable"
	"golang.org/x/term"

	"github.com/otnc/vmeltype/internal/appctl"
	"github.com/otnc/vmeltype/internal/assets"
	"github.com/otnc/vmeltype/internal/catalog/remote"
	"github.com/otnc/vmeltype/internal/cli"
	"github.com/otnc/vmeltype/internal/keyinput"
	"github.com/otnc/vmeltype/internal/osproc"
	"github.com/otnc/vmeltype/internal/rootdir"
	"github.com/otnc/vmeltype/internal/upstreamremove"
	"github.com/otnc/vmeltype/internal/winime"
	"github.com/otnc/vmeltype/internal/winsys"
)

// version は、配布のビルドで -ldflags "-X main.version=..." で埋め込む。
var version = "dev"

// stopAppTimeout は、Meltype に終了を求めてから終わるまで待つ時間。本家は合図のあと学習データを書いてから終わる。
const stopAppTimeout = 15 * time.Second

// removeMeltypeTimeout は、本家のアンインストーラーが Meltype を消し終えるまで待つ時間。本家の画面で確認に答える時間も含める。
const removeMeltypeTimeout = 10 * time.Minute

// colorEnabled は、f に色を付けてよいかを返す。NO_COLOR (https://no-color.org) と TERM=dumb を守り、端末でなければ付けない。
func colorEnabled(f *os.File) bool {
	return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && term.IsTerminal(int(f.Fd()))
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	env := cli.Env{
		In: os.Stdin,
		// go-colorable は、色の制御に対応していない Windows のコンソールでも色を付け、対応していれば素通しする
		Out: colorable.NewColorableStdout(),
		Err: colorable.NewColorableStderr(),
		// 確認は標準エラーに出すので、標準出力ではなく標準エラーが TTY かを見る
		Interactive: term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd())),
		OutColor:    colorEnabled(os.Stdout),
		ErrColor:    colorEnabled(os.Stderr),
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
		StartApp:           appctl.Start,
		LocalAppData:       winsys.LocalAppData,
		IMERegistered:      winsys.IMERegistered,
		InstalledTipHashes: winsys.InstalledTipHashes,
		ReplaceIME: func(appDir string) (winime.Result, error) {
			return winime.Replace(ctx, appDir)
		},
		Executable: os.Executable,
		RemoveMeltype: func(appDir, fallbackScript string, interactive bool) error {
			_, err := upstreamremove.Run(ctx, appDir, fallbackScript, interactive, removeMeltypeTimeout)
			return err
		},
	}
	// keyinput は、入力が端末でないと戻らないので、対話できるときだけ使う
	if env.Interactive {
		env.ListenKeys = keyinput.Listen
	}
	code := cli.RunContext(ctx, os.Args[1:], env)
	stop()
	os.Exit(code)
}
