// Package prompt は、変更を実行する前の確認を扱う。
package prompt

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Default は、空の Enter で選ばれる答え。
type Default int

const (
	// DefaultYes は [Y/n]。install、use、apply など、取り消せる変更に使う。
	DefaultYes Default = iota
	// DefaultNo は [y/N]。削除や復元など、失うものがある変更に使う。
	DefaultNo
)

var (
	// ErrCanceled は、変更を始める前に利用者が中止したことを表す。
	ErrCanceled = errors.New("中止しました")
	// ErrNeedsConfirmation は、確認できない環境で -y が指定されていないことを表す。
	ErrNeedsConfirmation = errors.New("確認が必要です。対話できない環境では -y を指定してください")
)

const escape = "\x1b"

// Confirmer は、計画を表示したあとの実行確認を行う。
// Interactive は、標準入力と表示先の両方が TTY のときだけ true にする。
type Confirmer struct {
	In          io.Reader
	Out         io.Writer
	Interactive bool
	AssumeYes   bool
}

// Confirm は、実行してよければ nil を返す。
// EOF は既定が Yes でも中止とみなし、入力が閉じた環境で勝手に実行しない。
//
//	err := c.Confirm("v1.4.0 を取得しますか？", prompt.DefaultYes)
//	if errors.Is(err, prompt.ErrCanceled) { ... }
func (c Confirmer) Confirm(question string, def Default) error {
	if c.AssumeYes {
		return nil
	}
	if !c.Interactive {
		return ErrNeedsConfirmation
	}
	yes, err := c.Ask(question, def)
	if err != nil {
		return err
	}
	if !yes {
		return ErrCanceled
	}
	return nil
}

// Ask は、y か n で答える質問をし、答えを返す。空の Enter は def を選ぶ。
// EOF と Esc は、n とは区別して ErrCanceled を返す (入力が閉じた環境で、どちらかを勝手に選ばない)。
// 続けて聞いたときに次の答えを読み落とさないよう、1 行ずつだけ読む。呼び出し側は、対話できることを確かめておく。
//
//	removeData, err := c.Ask("データも削除しますか？", prompt.DefaultNo)
func (c Confirmer) Ask(question string, def Default) (bool, error) {
	suffix := "[Y/n]"
	if def == DefaultNo {
		suffix = "[y/N]"
	}
	for {
		fmt.Fprintf(c.Out, "%s %s ", question, suffix)
		line, err := readLine(c.In)
		if err != nil && (!errors.Is(err, io.EOF) || line == "") {
			return false, ErrCanceled
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		atEOF := errors.Is(err, io.EOF)
		switch {
		case strings.Contains(answer, escape):
			return false, ErrCanceled
		case answer == "" && atEOF:
			return false, ErrCanceled
		case answer == "":
			return def == DefaultYes, nil
		case answer == "y" || answer == "yes":
			return true, nil
		case answer == "n" || answer == "no":
			return false, nil
		}
		if atEOF {
			return false, ErrCanceled
		}
		fmt.Fprintln(c.Out, "y か n で答えてください。")
	}
}

// readLine は、改行までを 1 バイトずつ読む。bufio で先読みすると、次の質問の答えまで読んでしまうため。
func readLine(r io.Reader) (string, error) {
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := r.Read(b)
		if n > 0 {
			if b[0] == '\n' {
				return string(line), nil
			}
			line = append(line, b[0])
		}
		if err != nil {
			return string(line), err
		}
	}
}
