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
	exitRequested := filepath.Join(dir, "exit-requested")
	if len(os.Args) > 1 && os.Args[1] == "--exit" {
		if err := os.WriteFile(exitRequested, nil, 0o600); err != nil {
			os.Exit(1)
		}
		return
	}
	// 合図を無視する Meltype を真似るとき。それでも 60 秒で終わり、テストのあとに残らない
	_, err = os.Stat(filepath.Join(dir, "ignore-exit"))
	ignoreExit := err == nil
	for range 600 {
		if _, err := os.Stat(exitRequested); err == nil && !ignoreExit {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
