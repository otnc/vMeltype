//go:build windows

package osproc

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// RunningUnder は、dir の下の実行ファイルから起動したプロセスの、実行ファイルのパスを返す。
// 他の利用者のプロセスなど、読めないものは飛ばす。
func RunningUnder(dir string) ([]string, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var found []string
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		path, ok := imagePath(entry.ProcessID)
		if ok && under(path, dir) {
			found = append(found, path)
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return found, nil
}

func imagePath(pid uint32) (string, bool) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(process)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(process, 0, &buf[0], &size); err != nil {
		return "", false
	}
	return windows.UTF16ToString(buf[:size]), true
}
