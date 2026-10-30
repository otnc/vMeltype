//go:build windows

package winsys

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

// InstalledTipHashes は、本家が Program Files に置いた Meltype IME の DLL の、署名する前の中身のハッシュを、CPU (x64、x86) ごとに返す。
// 本家は DLL を登録できたときだけ、DLL の隣に MeltypeTip.dll.sha256 を置く。ファイルがなければ、その CPU は空にする。
// Program Files の場所は、本家と同じく、利用者が上書きできる環境変数ではなく HKLM から読む。
func InstalledTipHashes() (map[string]string, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return nil, fmt.Errorf("Program Files の場所を読めませんでした: %w", err)
	}
	defer key.Close()
	programFiles, _, err := key.GetStringValue("ProgramFilesDir")
	if err != nil {
		return nil, fmt.Errorf("Program Files の場所を読めませんでした: %w", err)
	}
	hashes := map[string]string{}
	for _, arch := range []string{"x64", "x86"} {
		data, err := os.ReadFile(filepath.Join(programFiles, "Meltype", "tip", arch, "MeltypeTip.dll.sha256"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		hashes[arch] = NormalizeHash(string(data))
	}
	return hashes, nil
}
