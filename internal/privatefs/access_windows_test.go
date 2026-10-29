//go:build windows

package privatefs

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// othersCanAccess は、本人、SYSTEM、Administrators のほかに許可された主体があれば、その SDDL を返す。
func othersCanAccess(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sddl := sd.String()
	// ディレクトリは親から継承しない。中のファイルは、そのディレクトリから継承してよい
	if info, err := os.Stat(path); err == nil && info.IsDir() && !strings.Contains(sddl, "D:P") {
		return "継承を止めていない: " + sddl
	}
	rest := sddl
	for _, allowed := range []string{user.User.Sid.String(), ";SY)", ";BA)"} {
		rest = strings.ReplaceAll(rest, allowed, "")
	}
	for _, other := range []string{";WD)", ";BU)", ";AU)", ";S-1-"} {
		if strings.Contains(rest, other) {
			return sddl
		}
	}
	return ""
}
