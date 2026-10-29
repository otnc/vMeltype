package prompt

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestConfirm(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		interactive bool
		assumeYes   bool
		def         Default
		wantErr     error
		wantAsks    int
	}{
		{name: "既定が Yes なら空の Enter で実行する", input: "\n", interactive: true, def: DefaultYes, wantAsks: 1},
		{name: "既定が No なら空の Enter で中止する", input: "\n", interactive: true, def: DefaultNo, wantErr: ErrCanceled, wantAsks: 1},
		{name: "y で実行する", input: "y\n", interactive: true, def: DefaultNo, wantAsks: 1},
		{name: "yes と大文字も受け付ける", input: "YES\n", interactive: true, def: DefaultNo, wantAsks: 1},
		{name: "n で中止する", input: "n\n", interactive: true, def: DefaultYes, wantErr: ErrCanceled, wantAsks: 1},
		{name: "EOF は既定が Yes でも中止する", input: "", interactive: true, def: DefaultYes, wantErr: ErrCanceled, wantAsks: 1},
		{name: "空白だけのあとの EOF は既定が Yes でも中止する", input: " ", interactive: true, def: DefaultYes, wantErr: ErrCanceled, wantAsks: 1},
		{name: "改行のない入力のあとの EOF でも、その入力を使う", input: "y", interactive: true, def: DefaultNo, wantAsks: 1},
		{name: "Esc で中止する", input: "\x1b\n", interactive: true, def: DefaultYes, wantErr: ErrCanceled, wantAsks: 1},
		{name: "不明な入力では聞き直す", input: "maybe\ny\n", interactive: true, def: DefaultNo, wantAsks: 2},
		{name: "CRLF の改行でも読める", input: "\r\n", interactive: true, def: DefaultYes, wantAsks: 1},
		{name: "-y があれば聞かずに実行する", input: "", interactive: true, assumeYes: true, def: DefaultNo, wantAsks: 0},
		{name: "非 TTY で -y がなければ要操作で止まる", input: "y\n", interactive: false, def: DefaultYes, wantErr: ErrNeedsConfirmation, wantAsks: 0},
		{name: "非 TTY でも -y があれば実行する", input: "", interactive: false, assumeYes: true, def: DefaultYes, wantAsks: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			c := Confirmer{In: strings.NewReader(tt.input), Out: &out, Interactive: tt.interactive, AssumeYes: tt.assumeYes}

			err := c.Confirm("v1.4.0 を取得しますか？", tt.def)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if asks := strings.Count(out.String(), "v1.4.0 を取得しますか？"); asks != tt.wantAsks {
				t.Errorf("質問を %d 回表示した (want %d): %q", asks, tt.wantAsks, out.String())
			}
		})
	}
}

func TestConfirmShowsDefaultInSuffix(t *testing.T) {
	tests := []struct {
		name string
		def  Default
		want string
	}{
		{name: "既定が Yes なら [Y/n] を付ける", def: DefaultYes, want: "取得しますか？ [Y/n] "},
		{name: "既定が No なら [y/N] を付ける", def: DefaultNo, want: "削除しますか？ [y/N] "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			question, _, _ := strings.Cut(tt.want, " [")
			c := Confirmer{In: strings.NewReader("y\n"), Out: &out, Interactive: true}

			_ = c.Confirm(question, tt.def)

			if out.String() != tt.want {
				t.Errorf("表示 = %q, want %q", out.String(), tt.want)
			}
		})
	}
}
