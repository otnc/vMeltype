package cli

import (
	"regexp"
	"strings"
	"testing"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string {
	return ansi.ReplaceAllString(s, "")
}

func TestColor(t *testing.T) {
	t.Run("エラーは、標準エラーが色を付けられるときだけ色を付ける", func(t *testing.T) {
		for _, errColor := range []bool{true, false} {
			l := newLocalEnv(t)
			l.errColor = errColor

			got := l.run(t, false, "", "snapshot", "inspect", "20990101T000000Z")

			if colored := ansi.MatchString(got.stderr); colored != errColor || !strings.Contains(stripANSI(got.stderr), "エラー:") {
				t.Errorf("errColor = %v なのに、色 = %v: %q", errColor, colored, got.stderr)
			}
		}
	})
	t.Run("色を付けても、文字と桁は色なしと同じ", func(t *testing.T) {
		for _, args := range [][]string{{"app", "config", "show"}, {"doctor"}} {
			plain, colored := newLocalEnv(t), newLocalEnv(t)
			colored.root, colored.localAppData = plain.root, plain.localAppData
			colored.outColor = true

			want, got := plain.run(t, false, "", args...), colored.run(t, false, "", args...)

			if !ansi.MatchString(got.stdout) || stripANSI(got.stdout) != want.stdout {
				t.Errorf("%v: 色を付けていないか、色を除いた表示が違う:\n%q\n%q", args, got.stdout, want.stdout)
			}
		}
	})
	t.Run("--json には色を付けない", func(t *testing.T) {
		l := newLocalEnv(t)
		l.outColor = true

		got := l.run(t, false, "", "doctor", "--json")

		if ansi.MatchString(got.stdout) {
			t.Errorf("JSON に色を付けた: %q", got.stdout)
		}
	})
}

func TestWriteTableUsesDisplayWidth(t *testing.T) {
	var out strings.Builder
	writeTable(&out, style{}, [][]string{{"名前", "値"}, {"🍣", "a"}, {"abc", "b"}})

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	for _, line := range lines {
		if !strings.HasSuffix(line, "  値") && !strings.HasSuffix(line, "  a") && !strings.HasSuffix(line, "  b") {
			t.Errorf("列の間が 2 桁でない: %q", line)
		}
	}
	if want := "🍣    a"; lines[1] != want {
		t.Errorf("絵文字の幅を 2 として桁をそろえていない: %q, want %q", lines[1], want)
	}
}
