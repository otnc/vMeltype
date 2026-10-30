package keyinput

import (
	"testing"

	"github.com/otnc/vmeltype/internal/prompt"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want prompt.Key
	}{
		{name: "文字はそのまま", in: "y", want: prompt.Key{Kind: prompt.KeyRune, Rune: 'y'}},
		{name: "ASCII でない 1 文字も文字", in: "は", want: prompt.Key{Kind: prompt.KeyRune, Rune: 'は'}},
		{name: "貼り付けなどの複数の文字は答えに使わない", in: "yes", want: prompt.Key{Kind: prompt.KeyOther}},
		{name: "CR は Enter", in: "\r", want: prompt.Key{Kind: prompt.KeyEnter}},
		{name: "LF も Enter", in: "\n", want: prompt.Key{Kind: prompt.KeyEnter}},
		{name: "Esc だけなら Esc", in: "\x1b", want: prompt.Key{Kind: prompt.KeyEscape}},
		{name: "Esc で始まる矢印キーは答えに使わない", in: "\x1b[A", want: prompt.Key{Kind: prompt.KeyOther}},
		{name: "Ctrl+C は中断", in: "\x03", want: prompt.Key{Kind: prompt.KeyInterrupt}},
		{name: "Ctrl+\\ も中断", in: "\x1c", want: prompt.Key{Kind: prompt.KeyInterrupt}},
		{name: "Ctrl+D は入力の終わり", in: "\x04", want: prompt.Key{Kind: prompt.KeyEnd}},
		{name: "Ctrl+Z も入力の終わり", in: "\x1a", want: prompt.Key{Kind: prompt.KeyEnd}},
		{name: "ほかの制御文字は答えに使わない", in: "\t", want: prompt.Key{Kind: prompt.KeyOther}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decode([]byte(tt.in)); got != tt.want {
				t.Errorf("decode(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}
