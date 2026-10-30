//go:build windows

package appctl

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach は、vmeltype の終了や Ctrl+C を、起動した Meltype に伝えない。
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
}
