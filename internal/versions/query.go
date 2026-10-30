package versions

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrInvalidQuery は、バージョンの指定を解釈できないことを表す。
var ErrInvalidQuery = errors.New("バージョンの指定を解釈できません")

// 1、1.4、1.4.0、1.4.1-pre.1 の形。ビルドの情報 (+...) は比べないので受け付けない。
var queryPattern = regexp.MustCompile(`^(\d+)(?:\.(\d+)(?:\.(\d+)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?)?)?$`)

// Query は、バージョンの指定。省略、範囲 (1、1.4)、正式版 (1.4.0)、プレリリース (1.4.1-pre.1) を表す。
type Query struct {
	// parts は、指定した数字の数 (0 から 3)。0 は範囲の指定がないことを表す。
	parts               int
	major, minor, patch int
	prerelease          string
	// wantPrerelease は、--pre で、プレリリースだけから選ぶことを表す。
	wantPrerelease bool
}

// ParseQuery は、バージョンの指定を解釈する。先頭の v は省ける。空と latest は、範囲の指定がないことを表す。
// pre は --pre の有無で、正式版ではなくプレリリースだけから選ぶ。
//
//	q, err := versions.ParseQuery("1.4", false) // 1.4.x の正式版の最新
func ParseQuery(input string, pre bool) (Query, error) {
	q := Query{wantPrerelease: pre}
	if input == "" || input == "latest" {
		return q, nil
	}
	m := queryPattern.FindStringSubmatch(strings.TrimPrefix(input, "v"))
	if m == nil {
		return Query{}, fmt.Errorf("%w: %q (例: 1.4.0、1.4、1、1.4.1-pre.1)", ErrInvalidQuery, input)
	}
	numbers := []*int{&q.major, &q.minor, &q.patch}
	for i, text := range m[1:4] {
		if text == "" {
			break
		}
		n, err := strconv.Atoi(text)
		if err != nil {
			return Query{}, fmt.Errorf("%w: %q", ErrInvalidQuery, input)
		}
		*numbers[i] = n
		q.parts = i + 1
	}
	q.prerelease = m[4]
	return q, nil
}

// IsExact は、1 つのバージョンだけを指す指定かを返す。uninstall のように、推測で選んではいけない操作に使う。
func (q Query) IsExact() bool {
	return q.parts == 3 && (q.prerelease != "" || !q.wantPrerelease)
}

// Describe は、指定の意味を利用者に示す文言を返す。
func (q Query) Describe() string {
	switch {
	case q.prerelease != "":
		return fmt.Sprintf("%d.%d.%d-%s", q.major, q.minor, q.patch, q.prerelease)
	case q.parts == 3 && !q.wantPrerelease:
		return fmt.Sprintf("%d.%d.%d", q.major, q.minor, q.patch)
	}
	kind := "正式版の最新"
	if q.wantPrerelease {
		kind = "プレリリースの最新"
	}
	switch q.parts {
	case 1:
		return fmt.Sprintf("%d.x.x の%s", q.major, kind)
	case 2:
		return fmt.Sprintf("%d.%d.x の%s", q.major, q.minor, kind)
	case 3:
		return fmt.Sprintf("%d.%d.%d の%s", q.major, q.minor, q.patch, kind)
	}
	return kind
}

// Candidate は、選ぶ対象の 1 つ。Prerelease は、GitHub のプレリリースの印。
type Candidate struct {
	Tag        string
	Prerelease bool
}

// Select は、指定に合う候補のうち、SemVer が最大のものを返す。
// 解析できないタグは選ばない。公開の順や GitHub の Latest の表示には依らない。
func Select(candidates []Candidate, q Query) (Candidate, bool) {
	var best Candidate
	found := false
	for _, c := range candidates {
		if !q.matches(c) {
			continue
		}
		if !found || Compare(c.Tag, best.Tag) > 0 {
			best, found = c, true
		}
	}
	return best, found
}

func (q Query) matches(c Candidate) bool {
	m := queryPattern.FindStringSubmatch(strings.TrimPrefix(c.Tag, "v"))
	if m == nil || m[2] == "" || m[3] == "" {
		return false
	}
	version := []int{0, 0, 0}
	for i := range version {
		version[i], _ = strconv.Atoi(m[i+1])
	}
	tagPrerelease := m[4]
	if q.prerelease != "" {
		return version[0] == q.major && version[1] == q.minor && version[2] == q.patch && tagPrerelease == q.prerelease
	}
	// GitHub の印と、タグの - の後ろのどちらかがあれば、プレリリースとみなす
	isPrerelease := c.Prerelease || tagPrerelease != ""
	if isPrerelease != q.wantPrerelease {
		return false
	}
	wanted := []int{q.major, q.minor, q.patch}
	for i := 0; i < q.parts; i++ {
		if version[i] != wanted[i] {
			return false
		}
	}
	return true
}

// Newer は、current より新しい候補のうち、SemVer が最大のものを返す。いまより古いバージョンには下げない。
// includePre が false なら正式版だけ、true ならプレリリースと正式版を合わせた中から選ぶ。
func Newer(candidates []Candidate, current string, includePre bool) (Candidate, bool) {
	var best Candidate
	found := false
	for _, c := range candidates {
		if !IsValid(c.Tag) || Compare(c.Tag, current) <= 0 {
			continue
		}
		if !includePre && (c.Prerelease || strings.Contains(c.Tag, "-")) {
			continue
		}
		if !found || Compare(c.Tag, best.Tag) > 0 {
			best, found = c, true
		}
	}
	return best, found
}
