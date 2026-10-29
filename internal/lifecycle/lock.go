package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/otnc/vmeltype/internal/privatefs"
)

// ErrLocked は、別の vMeltype が同じ管理ルートを変更中であることを表す。
var ErrLocked = errors.New("別の vMeltype が同じ管理ルートを変更中です。終わってから実行してください")

// Lock は、管理ルートの変更の排他。OS のファイルロックなので、異常終了してもロックは残らない。
type Lock struct {
	file *os.File
}

// AcquireLock は、管理ルートの排他を待たずに取る。取れなければ ErrLocked を返す。
func AcquireLock(root string) (*Lock, error) {
	dir := stateDir(root)
	if err := privatefs.MkdirAll(dir); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("排他のファイルを開けませんでした: %w", err)
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, ErrLocked
	}
	return &Lock{file: f}, nil
}

// Release は、排他を解く。
func (l *Lock) Release() error {
	unlockFile(l.file)
	return l.file.Close()
}
