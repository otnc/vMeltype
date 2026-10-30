package prompt

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInterrupted は、確認の途中で Ctrl+C などで中断されたことを表す。
var ErrInterrupted = errors.New("中断しました")

// KeyKind は、1 つのキー入力の種類。
type KeyKind int

const (
	// KeyOther は、答えに使わないキー。
	KeyOther KeyKind = iota
	// KeyRune は、文字のキー。
	KeyRune
	// KeyEnter は、Enter。
	KeyEnter
	// KeyEscape は、Esc だけを押したこと。矢印キーなどは含めない。
	KeyEscape
	// KeyInterrupt は、Ctrl+C などの中断のキー。
	KeyInterrupt
	// KeyEnd は、Ctrl+D などの入力の終わりのキー。
	KeyEnd
)

// Key は、1 つのキー入力。
type Key struct {
	Kind KeyKind
	Rune rune
}

// ListenKeys は、端末を Enter を待たずに 1 キーずつ読み、handle が true を返すまで渡し続ける。終わったら端末を元に戻す。
type ListenKeys func(handle func(Key) (stop bool)) error

// askByKey は、y か n を押した時点で答えを決め、表示の [y/N] を選んだ文字に書き換える。
// Enter は既定を選び、Esc と入力の終わりは中止、Ctrl+C は中断にする。ほかのキーは何も出さずに無視する。
func (c Confirmer) askByKey(question, suffix string, def Default) (bool, error) {
	fmt.Fprintf(c.Out, "%s %s ", question, suffix)
	answer, result := "", ErrCanceled
	err := c.ListenKeys(func(key Key) bool {
		switch {
		case key.Kind == KeyRune && (key.Rune == 'y' || key.Rune == 'Y'):
			answer = "y"
		case key.Kind == KeyRune && (key.Rune == 'n' || key.Rune == 'N'):
			answer = "n"
		case key.Kind == KeyEnter && def == DefaultYes:
			answer = "y"
		case key.Kind == KeyEnter:
			answer = "n"
		case key.Kind == KeyInterrupt:
			result = ErrInterrupted
		case key.Kind == KeyEscape, key.Kind == KeyEnd:
		default:
			return false
		}
		return true
	})
	if err != nil || answer == "" {
		fmt.Fprintln(c.Out)
		return false, errors.Join(result, err)
	}
	// \b で [y/N] の先頭まで戻って答えを書き、残りを空白で消す。ANSI の制御に対応していない端末でも崩れない
	width := len(suffix) + 1
	fmt.Fprintf(c.Out, "%s%s%s\n", strings.Repeat("\b", width), answer, strings.Repeat(" ", width-1))
	return answer == "y", nil
}
