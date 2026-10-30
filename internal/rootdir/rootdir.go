// Package rootdir は、Meltool の管理ルートの既定の場所を決める。
package rootdir

import (
	"errors"
	"path/filepath"
	"strings"
)

// Default は、OS ごとの管理ルートの既定の場所を返す。
// 取得のたびに同じ場所になるよう、作業ディレクトリに依る相対パスは使わない。
//
//	root, err := rootdir.Default(runtime.GOOS, os.Getenv, home)
func Default(goos string, getenv func(string) string, home string) (string, error) {
	switch goos {
	case "windows":
		base := getenv("LOCALAPPDATA")
		if !isAbs(goos, base) {
			return "", errors.New("LOCALAPPDATA が設定されていないので、管理ルートを決められません。--root で指定してください")
		}
		return filepath.Join(base, "Meltool"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Meltool"), nil
	default:
		base := getenv("XDG_DATA_HOME")
		if !isAbs(goos, base) {
			base = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(base, "meltool"), nil
	}
}

// isAbs は、実行している OS ではなく goos の規則で絶対パスかを判定する。
func isAbs(goos, p string) bool {
	if goos == "windows" {
		hasDrive := len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
		return hasDrive || strings.HasPrefix(p, `\\`)
	}
	return strings.HasPrefix(p, "/")
}
