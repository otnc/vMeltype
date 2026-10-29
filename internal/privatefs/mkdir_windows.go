//go:build windows

package privatefs

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// mkdirPrivate は、本人、SYSTEM、Administrators だけに許可し、親から継承しない ACL を付けて作る。
// OICI で、中に作るファイルとディレクトリにも同じ許可を継承させる。
func mkdirPrivate(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("利用者の SID を取得できませんでした: %w", err)
	}
	sd, err := windows.SecurityDescriptorFromString(
		"D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)",
	)
	if err != nil {
		return err
	}
	attributes := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// ERROR_ALREADY_EXISTS は errors.Is(err, fs.ErrExist) で判定できる
	return windows.CreateDirectory(p, attributes)
}
