//go:build windows

package winsys

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalAppDataIsExistingDirectory(t *testing.T) {
	path, err := LocalAppData()
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if !filepath.IsAbs(path) || err != nil || !info.IsDir() {
		t.Errorf("LocalAppData = %q (%v)", path, err)
	}
}

func TestIMERegisteredReadsWithoutError(t *testing.T) {
	// 登録の有無は端末によるので、読めることだけを確かめる
	if _, err := IMERegistered(); err != nil {
		t.Error(err)
	}
}
