//go:build windows

package winsys

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// LocalAppData は、このユーザーの LocalAppData を返す。本家と同じく、利用者が上書きできる環境変数ではなく、Windows の既知のフォルダーから読む。
func LocalAppData() (string, error) {
	path, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", fmt.Errorf("LocalAppData の場所を読めませんでした: %w", err)
	}
	return path, nil
}

// IMERegistered は、Meltype IME が PC に登録されているかを返す。本家のインストーラーと同じく、64 ビットの HKLM の TIP のキーで判断する。
func IMERegistered() (bool, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\CTF\TIP\`+MeltypeTipCLSID, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("Meltype IME の登録を読めませんでした: %w", err)
	}
	key.Close()
	return true, nil
}
