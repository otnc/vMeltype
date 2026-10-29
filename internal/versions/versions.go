// Package versions は、本家のタグを SemVer として比べ、版の指定を exact tag に解決する。
package versions

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/otnc/vmeltype/internal/catalog"
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

// Latest は、区分の中で SemVer が最大のリリースを返す。includePrerelease なら正式版も含めて比べる。
// GitHub の Latest の表示や公開の順には依らない。
func Latest(releases []catalog.Release, includePrerelease bool) (catalog.Release, bool) {
	var best catalog.Release
	found := false
	for _, r := range releases {
		if (r.Prerelease && !includePrerelease) || !IsValid(r.Tag) {
			continue
		}
		if !found || Compare(r.Tag, best.Tag) > 0 {
			best, found = r, true
		}
	}
	return best, found
}

// ResolveTag は、版の指定を公式一覧のリリースに解決する。完全に一致するタグを優先し、
// 先頭の v の有無を補うのは、補った候補が 1 つだけのときに限る。
func ResolveTag(releases []catalog.Release, input string) (catalog.Release, error) {
	for _, r := range releases {
		if r.Tag == input {
			return r, nil
		}
	}
	alternative := "v" + input
	if strings.HasPrefix(input, "v") {
		alternative = strings.TrimPrefix(input, "v")
	}
	var candidates []catalog.Release
	for _, r := range releases {
		if r.Tag == alternative {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	return catalog.Release{}, fmt.Errorf("%w: %s", ErrTagNotFound, input)
}
