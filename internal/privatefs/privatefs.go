// Package privatefs は、管理ルートの中のディレクトリとファイルを、作った時点から本人だけが使える権限で作る。
// 作ったあとに権限を狭めると、その間に他の利用者が中身を読めてしまうため、作成と同時に権限を決める。
package privatefs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// MkdirAll は、path までの足りないディレクトリを、本人だけが使える権限で作る。
// すでにあるディレクトリの権限は変えない。
func MkdirAll(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	var missing []string
	for dir := path; ; dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("%s はディレクトリではありません", dir)
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		missing = append(missing, dir)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := mkdirPrivate(missing[i]); err != nil && !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s を作れませんでした: %w", missing[i], err)
		}
	}
	return nil
}

// WriteFile は、本人だけが使える新しいファイルとして data を書く。
// Windows では、親のディレクトリの ACL を継承する。
func WriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
