// Package prompt は、変更を実行する前の確認を扱う。
package prompt

import (
	"bufio"
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

	suffix := "[Y/n]"
	if def == DefaultNo {
		suffix = "[y/N]"
	}
	reader := bufio.NewReader(c.In)
	for {
		fmt.Fprintf(c.Out, "%s %s ", question, suffix)
		line, err := reader.ReadString('\n')
		if err != nil && (!errors.Is(err, io.EOF) || line == "") {
			return ErrCanceled
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		atEOF := errors.Is(err, io.EOF)
		switch {
		case answer == "" && atEOF:
			return ErrCanceled
		case answer == "":
			if def == DefaultYes {
				return nil
			}
			return ErrCanceled
		case answer == "y" || answer == "yes":
			return nil
		case answer == "n" || answer == "no" || strings.Contains(answer, escape):
			return ErrCanceled
		}
		if atEOF {
			return ErrCanceled
		}
		fmt.Fprintln(c.Out, "y か n で答えてください。")
	}
}
