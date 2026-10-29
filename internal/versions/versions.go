// Package versions は、本家のタグを SemVer として比べ、版の指定 (範囲を含む) からタグを選ぶ。
package versions

import (
	"errors"
	"strings"

	"golang.org/x/mod/semver"
)

// ErrTagNotFound は、指定した版が公式一覧にないことを表す。
var ErrTagNotFound = errors.New("指定した版は公式一覧にありません")

// comparable は、比較のためだけに先頭へ v を補った値を返す。元のタグは書き換えない。
func comparable(tag string) string {
	if strings.HasPrefix(tag, "v") {
		return tag
	}
	return "v" + tag
}

// IsValid は、タグを SemVer として比べられるかを返す。
func IsValid(tag string) bool {
	return semver.IsValid(comparable(tag))
}

// Compare は、2 つのタグを SemVer として比べる。解析できないタグは、どのタグよりも小さいとみなす。
func Compare(a, b string) int {
	return semver.Compare(comparable(a), comparable(b))
}
