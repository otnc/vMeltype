package prompt

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// fakeKeys は、端末のキー入力の代わりに、決めた順にキーを渡す。渡し終えたら、入力が閉じたときと同じく何も決めずに戻る。
type fakeKeys struct {
	keys []Key
}

func (f *fakeKeys) listen(handle func(Key) bool) error {
	for _, k := range f.keys {
		if handle(k) {
			return nil
		}
	}
	return nil
}

func runes(s string) []Key {
	var keys []Key
	for _, r := range s {
		keys = append(keys, Key{Kind: KeyRune, Rune: r})
	}
	return keys
}

// rendered は、端末と同じく \b で 1 文字戻って上書きしたときに、画面に残る行を返す。
func rendered(out string) []string {
	var lines []string
	for _, raw := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		var line []rune
		pos := 0
		for _, r := range raw {
			if r == '\b' {
				pos--
				continue
			}
			if pos < len(line) {
				line[pos] = r
			} else {
				line = append(line, r)
			}
			pos++
		}
		lines = append(lines, strings.TrimRight(string(line), " "))
	}
	return lines
}

func TestAskByKey(t *testing.T) {
	enter, esc, interrupt := Key{Kind: KeyEnter}, Key{Kind: KeyEscape}, Key{Kind: KeyInterrupt}
	tests := []struct {
		name     string
		keys     []Key
		def      Default
		want     bool
		wantErr  error
		wantLine string
	}{
		{name: "y を押せば Enter なしで決まり、表示を y にする", keys: runes("y"), def: DefaultNo, want: true, wantLine: "削除しますか？ y"},
		{name: "n を押せば n にする", keys: runes("n"), def: DefaultYes, want: false, wantLine: "削除しますか？ n"},
		{name: "大文字の Y も受け付ける", keys: runes("Y"), def: DefaultNo, want: true, wantLine: "削除しますか？ y"},
		{name: "Enter は既定が No なら n にする", keys: []Key{enter}, def: DefaultNo, want: false, wantLine: "削除しますか？ n"},
		{name: "Enter は既定が Yes なら y にする", keys: []Key{enter}, def: DefaultYes, want: true, wantLine: "削除しますか？ y"},
		{name: "ほかのキーは何も出さずに無視する", keys: append(runes("xq1 "), runes("y")...), def: DefaultNo, want: true, wantLine: "削除しますか？ y"},
		{name: "Esc で中止する", keys: []Key{esc}, def: DefaultYes, wantErr: ErrCanceled, wantLine: "削除しますか？ [Y/n]"},
		{name: "Ctrl+C で中断する", keys: []Key{interrupt}, def: DefaultYes, wantErr: ErrInterrupted, wantLine: "削除しますか？ [Y/n]"},
		{name: "Ctrl+D で中止する", keys: []Key{{Kind: KeyEnd}}, def: DefaultYes, wantErr: ErrCanceled, wantLine: "削除しますか？ [Y/n]"},
		{name: "入力が閉じたら、既定が Yes でも中止する", def: DefaultYes, wantErr: ErrCanceled, wantLine: "削除しますか？ [Y/n]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := &fakeKeys{keys: tt.keys}
			var out bytes.Buffer
			c := Confirmer{Out: &out, Interactive: true, ListenKeys: keys.listen}

			got, err := c.Ask("削除しますか？", tt.def)

			if !errors.Is(err, tt.wantErr) || (err == nil && got != tt.want) {
				t.Errorf("Ask = %v, %v, want %v, %v", got, err, tt.want, tt.wantErr)
			}
			if lines := rendered(out.String()); len(lines) != 1 || lines[0] != tt.wantLine {
				t.Errorf("表示 = %q, want %q", lines, tt.wantLine)
			}
			if !strings.HasSuffix(out.String(), "\n") {
				t.Errorf("答えたあとに改行していない: %q", out.String())
			}
		})
	}
}
