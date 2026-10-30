package lifecycle

import (
	"errors"
	"testing"
)

func TestAcquireLock(t *testing.T) {
	root := t.TempDir()
	first, err := AcquireLock(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := AcquireLock(root); !errors.Is(err, ErrLocked) {
		t.Errorf("取得中の排他をもう一度取れた: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	again, err := AcquireLock(root)
	if err != nil {
		t.Errorf("解いたあとに取れない: %v", err)
	} else {
		again.Release()
	}
}
