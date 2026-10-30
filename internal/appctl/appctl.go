// Package appctl は、実行先の Meltype に終了を求め、起動する。強制終了はしない。
package appctl

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExeName は、本家の Meltype の実行ファイルの名前。
const ExeName = "Meltype.exe"

// ErrStillRunning は、終了の合図を送っても、時間内に Meltype が終わらなかったことを表す。
var ErrStillRunning = errors.New("終了を求めましたが、Meltype が終わりませんでした。強制終了はしていません。Meltype を終了してから、もう一度実行してください")

const pollInterval = 200 * time.Millisecond

// Stop は、本家の "Meltype.exe --exit" で終了の合図を送り、実行先から動いているプロセスがなくなるまで、最長 timeout だけ待つ。
// 本家は合図を受けると、まだ書いていない学習データを書いてから終わるので、プロセスがなくなれば書き終えている。
// --exit のない版 (v0.1.0) や、合図に応えない Meltype は、強制終了せずに ErrStillRunning を返す。
func Stop(ctx context.Context, appDir string, runningUnder func(string) ([]string, error), timeout time.Duration) error {
	cmd := exec.CommandContext(ctx, filepath.Join(appDir, ExeName), "--exit")
	cmd.Dir = appDir
	// 合図を送れなくても (すでに終わりかけているなど)、終わったかどうかはプロセスで確かめる
	signalErr := cmd.Run()

	deadline := time.Now().Add(timeout)
	for {
		running, err := runningUnder(appDir)
		if err != nil {
			return fmt.Errorf("Meltype が終わったかを確かめられませんでした: %w", err)
		}
		if len(running) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			if signalErr != nil {
				return fmt.Errorf("%w (%s。終了の合図: %v)", ErrStillRunning, strings.Join(running, "、"), signalErr)
			}
			return fmt.Errorf("%w (%s)", ErrStillRunning, strings.Join(running, "、"))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// Start は、実行先の Meltype を起動し、終わるのを待たない。
func Start(appDir string) error {
	cmd := exec.Command(filepath.Join(appDir, ExeName))
	cmd.Dir = appDir
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Meltype を起動できませんでした: %w", err)
	}
	return cmd.Process.Release()
}
