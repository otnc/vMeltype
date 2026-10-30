// Package keyinput は、端末を Enter を待たずに 1 キーずつ読み、prompt のキーにして渡す。y/n の確認を 1 キーで答えられるようにするために使う。
// 端末の切り替えは golang.org/x/term に任せ、OS ごとの書き分けはしない。入力が端末でなければ、MakeRaw がエラーを返す。
package keyinput

import (
	"os"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/otnc/vmeltype/internal/prompt"
)

// 制御文字。端末を 1 キーずつ読む状態にすると、Ctrl+C なども文字として届く。
const (
	ctrlC         = 0x03
	ctrlD         = 0x04
	ctrlZ         = 0x1a
	ctrlBackslash = 0x1c
	escape        = 0x1b
)

// Listen は、標準入力のキーを handle に渡し、handle が true を返すか、読めなくなったら、端末を元に戻して終わる。
func Listen(handle func(prompt.Key) bool) error {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, old)
	buf := make([]byte, 16)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return err
		}
		if handle(decode(buf[:n])) {
			return nil
		}
	}
}

// decode は、1 回で読めたバイト列を 1 つのキーにする。
// 矢印キーなどは Esc で始まる並びがまとめて届くので、Esc の 1 バイトだけのときを Esc とみなす。
func decode(b []byte) prompt.Key {
	if len(b) == 1 {
		switch b[0] {
		case ctrlC, ctrlBackslash:
			return prompt.Key{Kind: prompt.KeyInterrupt}
		case ctrlD, ctrlZ:
			return prompt.Key{Kind: prompt.KeyEnd}
		case '\r', '\n':
			return prompt.Key{Kind: prompt.KeyEnter}
		case escape:
			return prompt.Key{Kind: prompt.KeyEscape}
		}
	}
	r, size := utf8.DecodeRune(b)
	if size != len(b) || r == utf8.RuneError || r < 0x20 || r == 0x7f {
		return prompt.Key{Kind: prompt.KeyOther}
	}
	return prompt.Key{Kind: prompt.KeyRune, Rune: r}
}
