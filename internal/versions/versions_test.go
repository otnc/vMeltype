package versions

import (
	"errors"
	"testing"

	"github.com/otnc/vmeltype/internal/catalog"
)

func releases(tags ...string) []catalog.Release {
	var rs []catalog.Release
	for i, tag := range tags {
		rs = append(rs, catalog.Release{ID: int64(i + 1), Tag: tag, Prerelease: isPre(tag)})
	}
	return rs
}

func isPre(tag string) bool {
	for _, r := range tag {
		if r == '-' {
			return true
		}
	}
	return false
}

func TestLatest(t *testing.T) {
	tests := []struct {
		name       string
		tags       []string
		prerelease bool
		want       string
		wantFound  bool
	}{
		{name: "正式版の区分では SemVer の最大の正式版", tags: []string{"v1.3.0", "v1.4.0", "v1.3.1-pre.3"}, want: "v1.4.0", wantFound: true},
		{name: "公開順ではなく SemVer で比べる", tags: []string{"v1.10.0", "v1.9.0"}, want: "v1.10.0", wantFound: true},
		{name: "プレリリースの区分は正式版も含めて最大を選ぶ", tags: []string{"v1.3.0", "v1.4.0", "v1.3.1-pre.3"}, prerelease: true, want: "v1.4.0", wantFound: true},
		{name: "プレリリースの区分で正式版より新しいプレリリースを選ぶ", tags: []string{"v1.3.0", "v1.4.0-pre.1"}, prerelease: true, want: "v1.4.0-pre.1", wantFound: true},
		{name: "数字の pre 番号を数として比べる", tags: []string{"v1.3.1-pre.2", "v1.3.1-pre.10"}, prerelease: true, want: "v1.3.1-pre.10", wantFound: true},
		{name: "解析できないタグは選ばない", tags: []string{"release-2026", "v1.0.0"}, want: "v1.0.0", wantFound: true},
		{name: "先頭に v のないタグも比べる", tags: []string{"1.5.0", "v1.4.0"}, want: "1.5.0", wantFound: true},
		{name: "候補がなければ見つからない", tags: []string{"v1.3.1-pre.3"}, wantFound: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := Latest(releases(tt.tags...), tt.prerelease)

			if found != tt.wantFound || (found && got.Tag != tt.want) {
				t.Errorf("got %q (%t), want %q (%t)", got.Tag, found, tt.want, tt.wantFound)
			}
		})
	}
}

func TestResolveTag(t *testing.T) {
	tests := []struct {
		name    string
		tags    []string
		input   string
		want    string
		wantErr error
	}{
		{name: "完全に一致するタグ", tags: []string{"v1.4.0"}, input: "v1.4.0", want: "v1.4.0"},
		{name: "v を補う", tags: []string{"v1.4.0"}, input: "1.4.0", want: "v1.4.0"},
		{name: "余計な v を外す", tags: []string{"1.4.0"}, input: "v1.4.0", want: "1.4.0"},
		{name: "完全に一致するものを補完より優先する", tags: []string{"1.4.0", "v1.4.0"}, input: "1.4.0", want: "1.4.0"},
		{name: "プレリリースのタグも --pre なしで指定できる", tags: []string{"v1.3.1-pre.3"}, input: "1.3.1-pre.3", want: "v1.3.1-pre.3"},
		{name: "前方一致では解決しない", tags: []string{"v1.4.0"}, input: "v1.4", wantErr: ErrTagNotFound},
		{name: "ないタグ", tags: []string{"v1.4.0"}, input: "v9.9.9", wantErr: ErrTagNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveTag(releases(tt.tags...), tt.input)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got.Tag != tt.want {
				t.Errorf("got %q, want %q", got.Tag, tt.want)
			}
		})
	}
}
