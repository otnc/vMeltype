package versions

import (
	"errors"
	"testing"
)

var sampleTags = []string{
	"v0.3.0",
	"v1.0.0",
	"v1.3.0",
	"v1.3.1-pre.1",
	"v1.3.1-pre.3",
	"v1.3.1-pre.2",
	"v1.4.0",
	"v1.4.1-pre.1",
	"v1.4.1-pre.2",
	"v2.0.0-pre.1",
	"nightly",
}

func candidates(tags []string) []Candidate {
	var cs []Candidate
	for _, tag := range tags {
		cs = append(cs, Candidate{Tag: tag, Prerelease: isPre(tag)})
	}
	return cs
}

func TestSelect(t *testing.T) {
	tests := []struct {
		name  string
		input string
		pre   bool
		want  string
	}{
		{name: "省略すると正式版の最新", input: "", want: "v1.4.0"},
		{name: "latest は省略と同じ", input: "latest", want: "v1.4.0"},
		{name: "メジャーだけなら、その範囲の正式版の最新", input: "1", want: "v1.4.0"},
		{name: "マイナーまでなら、その範囲の正式版の最新", input: "1.3", want: "v1.3.0"},
		{name: "先頭の v を付けても同じ", input: "v1.3", want: "v1.3.0"},
		{name: "3 つそろえば、その正式版", input: "1.0.0", want: "v1.0.0"},
		{name: "--pre はプレリリースだけから最新", pre: true, want: "v2.0.0-pre.1"},
		{name: "--pre と範囲で、その範囲のプレリリースの最新", input: "1", pre: true, want: "v1.4.1-pre.2"},
		{name: "--pre と 3 つの版で、その版のプレリリースの最新", input: "1.3.1", pre: true, want: "v1.3.1-pre.3"},
		{name: "明示したプレリリース", input: "1.3.1-pre.2", want: "v1.3.1-pre.2"},
		{name: "明示したプレリリースは --pre があっても同じ", input: "v1.3.1-pre.2", pre: true, want: "v1.3.1-pre.2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := ParseQuery(tt.input, tt.pre)
			if err != nil {
				t.Fatal(err)
			}

			got, ok := Select(candidates(sampleTags), q)

			if !ok || got.Tag != tt.want {
				t.Errorf("got %q (ok=%t), want %q", got.Tag, ok, tt.want)
			}
		})
	}
}

func TestSelectNotFound(t *testing.T) {
	tests := []struct {
		name  string
		input string
		pre   bool
	}{
		{name: "正式版のない範囲", input: "2"},
		{name: "ない正式版", input: "1.3.1"},
		{name: "プレリリースのない範囲で --pre", input: "1.3.0", pre: true},
		{name: "ないプレリリース", input: "1.3.1-pre.9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := ParseQuery(tt.input, tt.pre)
			if err != nil {
				t.Fatal(err)
			}
			if got, ok := Select(candidates(sampleTags), q); ok {
				t.Errorf("候補がないはずなのに %q を選んだ", got.Tag)
			}
		})
	}
}

func TestSelectIgnoresUnparsableTags(t *testing.T) {
	q, _ := ParseQuery("", false)

	got, ok := Select(candidates([]string{"nightly", "latest-build"}), q)

	if ok {
		t.Errorf("解析できないタグ %q を選んだ", got.Tag)
	}
}

func TestParseQueryRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"1.4.0.1", "x", "1.x", "v", "1..2", "-pre"} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseQuery(input, false); !errors.Is(err, ErrInvalidQuery) {
				t.Errorf("err = %v, want ErrInvalidQuery", err)
			}
		})
	}
}

func TestQueryIsExact(t *testing.T) {
	tests := []struct {
		input string
		pre   bool
		want  bool
	}{
		{input: "1.4.0", want: true},
		{input: "v1.4.1-pre.1", want: true},
		{input: "", want: false},
		{input: "latest", want: false},
		{input: "1", want: false},
		{input: "1.4", want: false},
		{input: "1.4.1", pre: true, want: false},
	}
	for _, tt := range tests {
		q, err := ParseQuery(tt.input, tt.pre)
		if err != nil {
			t.Fatal(err)
		}
		if q.IsExact() != tt.want {
			t.Errorf("%q (pre=%t): IsExact = %t, want %t", tt.input, tt.pre, q.IsExact(), tt.want)
		}
	}
}

func TestQueryDescribe(t *testing.T) {
	tests := []struct {
		input string
		pre   bool
		want  string
	}{
		{input: "", want: "正式版の最新"},
		{input: "1", want: "1.x.x の正式版の最新"},
		{input: "1.4", want: "1.4.x の正式版の最新"},
		{input: "1.4.0", want: "1.4.0"},
		{input: "", pre: true, want: "プレリリースの最新"},
		{input: "1.4.1", pre: true, want: "1.4.1 のプレリリースの最新"},
		{input: "v1.4.1-pre.1", want: "1.4.1-pre.1"},
	}
	for _, tt := range tests {
		q, _ := ParseQuery(tt.input, tt.pre)
		if got := q.Describe(); got != tt.want {
			t.Errorf("%q (pre=%t): %q, want %q", tt.input, tt.pre, got, tt.want)
		}
	}
}
