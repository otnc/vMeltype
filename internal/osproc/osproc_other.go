//go:build !windows

package osproc

import "errors"

// RunningUnder は、Windows 以外ではまだ扱わない。portable 版は Windows だけの配布物のため。
func RunningUnder(dir string) ([]string, error) {
	return nil, errors.New("この OS では、動いているプロセスをまだ調べられません")
}
