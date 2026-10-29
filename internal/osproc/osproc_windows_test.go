//go:build windows

package osproc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunningUnderFindsOwnProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	found, err := RunningUnder(filepath.Dir(exe))
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range found {
		if under(path, filepath.Dir(exe)) {
			return
		}
	}
	t.Errorf("自分のプロセス %s を見つけられない: %v", exe, found)
}

func TestRunningUnderIgnoresOtherDirectories(t *testing.T) {
	found, err := RunningUnder(t.TempDir())

	if err != nil || len(found) != 0 {
		t.Errorf("何も動いていないディレクトリで %v, %v", found, err)
	}
}
