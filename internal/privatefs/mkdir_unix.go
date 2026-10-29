//go:build !windows

package privatefs

import "os"

func mkdirPrivate(path string) error {
	return os.Mkdir(path, 0o700)
}
