// vmeltype は、Meltype の複数の版を保持し、切り替える CLI。
package main

import (
	"context"
	"os"
	"os/signal"

	"golang.org/x/term"

	"github.com/otnc/vmeltype/internal/cli"
)

// version は、配布のビルドで -ldflags "-X main.version=..." で埋め込む。
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	streams := cli.Streams{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
		// 確認は標準エラーに出すので、標準出力ではなく標準エラーが TTY かを見る
		Interactive: term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd())),
	}
	code := cli.RunContext(ctx, os.Args[1:], streams, version)
	stop()
	os.Exit(code)
}
