// fakemeltype は、テストで本物の Meltype の代わりに動く。本家の Meltype.exe --exit の合図だけを真似る。
package main

import (
	"os"
	"path/filepath"
	"time"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}
	dir := filepath.Dir(exe)
	ready := filepath.Join(dir, "ready")
	exitRequested := filepath.Join(dir, "exit-requested")
	if len(os.Args) > 1 && os.Args[1] == "--exit" {
		// 本家も、合図の受け口ができる前の Meltype には送れず、1 で終わる
		if !exists(ready) {
			os.Exit(1)
		}
		if err := os.WriteFile(exitRequested, nil, 0o600); err != nil {
			os.Exit(1)
		}
		return
	}
	// 起動してから合図を受け取れるまでに時間がかかる Meltype を真似るとき
	if exists(filepath.Join(dir, "slow-start")) {
		time.Sleep(time.Second)
	}
	os.WriteFile(ready, nil, 0o600)
	// 合図を無視する Meltype を真似るとき。それでも 60 秒で終わり、テストのあとに残らない
	ignoreExit := exists(filepath.Join(dir, "ignore-exit"))
	for range 600 {
		if exists(exitRequested) && !ignoreExit {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
