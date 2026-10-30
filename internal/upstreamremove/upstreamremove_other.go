//go:build !windows

package upstreamremove

import (
	"context"
	"errors"
	"time"
)

// Run は、Windows 以外では使わない。
func Run(context.Context, string, string, bool, time.Duration) (Method, error) {
	return "", errors.New("通常版の Meltype の削除は、Windows でだけ行えます")
}
