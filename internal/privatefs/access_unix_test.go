//go:build !windows

package privatefs

import (
	"fmt"
	"os"
	"testing"
)

func othersCanAccess(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Sprintf("権限 %o", perm)
	}
	return ""
}
