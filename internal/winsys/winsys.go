// Package winsys は、通常版の Meltype が使う Windows の場所と登録を読む。読むだけで、何も変えない。
package winsys

import "strings"

// MeltypeTipCLSID は、本家の Meltype IME (TSF の入力方式) の CLSID。
const MeltypeTipCLSID = "{417D801B-A9BD-4C26-BD16-356A825A6998}"

// NormalizeHash は、本家のハッシュのファイルの中身を、比べられる形 (前後の空白なし、大文字) にする。
func NormalizeHash(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}
