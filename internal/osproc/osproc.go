// Package osproc は、動いているプロセスを OS から読む。プロセスを止める機能は持たない。
package osproc

import (
	"path/filepath"
	"strings"
)

// under は、path が dir の下にあるかを、大文字と小文字を区別せずに返す (Windows のパスの規則)。
func under(path, dir string) bool {
	dir = filepath.Clean(dir) + string(filepath.Separator)
	return len(path) > len(dir) && strings.EqualFold(path[:len(dir)], dir)
}
