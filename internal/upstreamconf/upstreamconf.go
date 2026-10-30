// Package upstreamconf は、本家の設定 (データ先の config.json) のうち、Meltool が管理中に変える項目だけを書き換える。
// 本家は起動のたびに設定を読み直して保存するので、ファイル全体を読み直して書き出さず、対象の値だけを置き換える。
package upstreamconf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

const autoUpdateKey = "AutoUpdate"

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Original は、書き換える前の状態。管理前に戻すときに使う。AutoUpdate が nil なら、項目がなかった (本家の既定は true)。
type Original struct {
	FileExisted bool  `json:"fileExisted"`
	AutoUpdate  *bool `json:"autoUpdate,omitempty"`
}

// DisableAutoUpdate は、本家の自動更新を止める。ほかの項目、並び、空白は変えない。
// 読めない設定は、本家が既定の値で上書きするかもしれないので、推測で直さずにエラーにする。
// ファイルがなければ、自動更新だけを止めた設定を作る (本家はないときだけ既定の設定を保存するので、そのままだと自動更新が有効になる)。
// 呼び出し側は、書き換えの間に本家が保存しないよう、Meltype を止めておく。
func DisableAutoUpdate(path string) (Original, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return Original{}, writeAtomic(path, []byte(`{"AutoUpdate": false}`))
	}
	return setAutoUpdate(path, false)
}

// RestoreAutoUpdate は、本家の自動更新を、DisableAutoUpdate が記録した管理前の値に戻す。管理前に項目がなければ、本家の既定の true にする。
// ほかの項目、並び、空白は変えない。ファイルがなければ、本家が次の起動で既定の設定を作るので、何もしない。
// 呼び出し側は、書き換えの間に本家が保存しないよう、Meltype を止めておく。
func RestoreAutoUpdate(path string, original Original) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	value := true
	if original.AutoUpdate != nil {
		value = *original.AutoUpdate
	}
	_, err := setAutoUpdate(path, value)
	return err
}

// setAutoUpdate は、自動更新の値だけを置き換える。項目がなければ先頭に足す。書き換える前の状態を返す。
func setAutoUpdate(path string, want bool) (Original, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Original{}, fmt.Errorf("本家の設定を読めませんでした: %w", err)
	}
	original := Original{FileExisted: true}
	body := bytes.TrimPrefix(data, utf8BOM)
	bom := data[:len(data)-len(body)]
	literal := strconv.FormatBool(want)

	value, err := locateTopLevel(body, autoUpdateKey)
	if err != nil {
		return Original{}, fmt.Errorf("本家の設定 (%s) を読めないので、自動更新を変えられません: %w", path, err)
	}
	var updated []byte
	if value == nil {
		insertAt := bytes.IndexByte(body, '{') + 1
		entry := `"AutoUpdate": ` + literal
		if !emptyObject(body[insertAt:]) {
			entry += ","
		}
		updated = splice(body, insertAt, insertAt, entry)
	} else {
		var current bool
		if err := json.Unmarshal(body[value.start:value.end], &current); err != nil {
			return Original{}, fmt.Errorf("本家の設定 (%s) の %s が真偽値ではないので、自動更新を変えられません", path, autoUpdateKey)
		}
		original.AutoUpdate = &current
		if current == want {
			return original, nil
		}
		updated = splice(body, value.start, value.end, literal)
	}
	if err := verifyOnlyAutoUpdateChanged(body, updated, literal); err != nil {
		return Original{}, err
	}
	return original, writeAtomic(path, append(append([]byte{}, bom...), updated...))
}

type span struct{ start, end int }

// locateTopLevel は、オブジェクトの直下の key の値の位置を返す。なければ nil を返す。
func locateTopLevel(body []byte, key string) (*span, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	if tok, err := dec.Token(); err != nil {
		return nil, err
	} else if tok != json.Delim('{') {
		return nil, errors.New("JSON のオブジェクトではありません")
	}
	var found *span
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		afterKey := int(dec.InputOffset())
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		if tok == key {
			end := int(dec.InputOffset())
			start := afterKey + bytes.IndexByte(body[afterKey:end], ':') + 1
			for start < end && isSpace(body[start]) {
				start++
			}
			found = &span{start, end}
		}
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("JSON の後ろに余計なものがあります")
	}
	return found, nil
}

// verifyOnlyAutoUpdateChanged は、書き換えたあとも JSON として読め、自動更新が want になり、ほかは同じ値であることを確かめる。
func verifyOnlyAutoUpdateChanged(before, after []byte, want string) error {
	var b, a map[string]json.RawMessage
	if err := json.Unmarshal(before, &b); err != nil {
		return err
	}
	if err := json.Unmarshal(after, &a); err != nil {
		return fmt.Errorf("本家の設定を書き換えた結果が JSON として読めません: %w", err)
	}
	delete(b, autoUpdateKey)
	if string(a[autoUpdateKey]) != want {
		return errors.New("本家の設定の自動更新を変えられませんでした")
	}
	delete(a, autoUpdateKey)
	if len(a) != len(b) {
		return errors.New("本家の設定の自動更新のほかの項目が変わってしまいます")
	}
	for k, v := range b {
		if !bytes.Equal(a[k], v) {
			return fmt.Errorf("本家の設定の %s が変わってしまいます", k)
		}
	}
	return nil
}

func splice(body []byte, start, end int, text string) []byte {
	out := make([]byte, 0, len(body)+len(text))
	out = append(out, body[:start]...)
	out = append(out, text...)
	return append(out, body[end:]...)
}

func emptyObject(rest []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(rest, " \t\r\n"), []byte("}"))
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// writeAtomic は、同じディレクトリの一時ファイルへ書いてから置き換え、本家が書きかけの設定を読まないようにする。
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
