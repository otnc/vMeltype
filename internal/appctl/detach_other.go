//go:build !windows

package appctl

import "os/exec"

func detach(*exec.Cmd) {}
