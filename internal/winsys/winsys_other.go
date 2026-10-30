//go:build !windows

package winsys

import "errors"

var errNotWindows = errors.New("通常版の Windows の場所と登録は、Windows でだけ読めます")

// LocalAppData は、Windows 以外では使わない。
func LocalAppData() (string, error) {
	return "", errNotWindows
}

// IMERegistered は、Windows 以外では使わない。
func IMERegistered() (bool, error) {
	return false, errNotWindows
}
