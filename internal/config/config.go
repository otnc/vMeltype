// Package config は、管理ルートに置く vMeltype の設定を読み書きする。通信しない。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/otnc/vmeltype/internal/privatefs"
)

// ErrInvalid は、Set に渡した項目か値を受け付けないことを表す。
var ErrInvalid = errors.New("設定できません")

// Settings は、vMeltype が使う設定。ファイルにない項目は既定の値になる。
type Settings struct {
	// StopAppOnYes は、-y だけで、動いている実行先の Meltype を終了させるか。
	StopAppOnYes bool
	// AutoSnapshotKeep は、自動で作ったスナップショットを、新しいほうからいくつ残すか。0 なら消さない。
	AutoSnapshotKeep int
}

// Item は、show で表示する 1 つの設定。
type Item struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

// key は、1 つの設定の型と既定の値。値は、ファイルでは JSON の値、利用者の入力と表示では文字列で扱う。
type key struct {
	defaultValue string
	description  string
	// hint は、受け付けない値を渡されたときに示す、受け付ける値の説明。
	hint string
	// parse は、利用者の入力を JSON の値にする。受け付けなければ false を返す。
	parse func(string) (any, bool)
	// apply は、ファイルの値を Settings に入れ、表示する文字列を返す。
	apply func(*Settings, json.RawMessage) (string, error)
}

var keys = map[string]key{
	"stop-app-on-yes": {
		defaultValue: "true",
		description:  "-y だけで、動いている実行先の Meltype を終了させる。false なら --stop-app を付けたときだけ終了させる",
		hint:         "true か false",
		parse: func(s string) (any, bool) {
			v, err := strconv.ParseBool(s)
			return v, err == nil
		},
		apply: func(s *Settings, data json.RawMessage) (string, error) {
			err := json.Unmarshal(data, &s.StopAppOnYes)
			return strconv.FormatBool(s.StopAppOnYes), err
		},
	},
	"auto-snapshot-keep": {
		defaultValue: "10",
		description:  "切り替えや復元の前に自動で作ったスナップショットを、新しいほうからいくつ残すか。0 なら消さない。手動で作ったものと取り込んだものは数えない",
		hint:         "0 以上の整数",
		parse: func(s string) (any, bool) {
			v, err := strconv.Atoi(s)
			return v, err == nil && v >= 0
		},
		apply: func(s *Settings, data json.RawMessage) (string, error) {
			if err := json.Unmarshal(data, &s.AutoSnapshotKeep); err != nil {
				return "", err
			}
			if s.AutoSnapshotKeep < 0 {
				return "", errors.New("負の数")
			}
			return strconv.Itoa(s.AutoSnapshotKeep), nil
		},
	},
}

func path(root string) string {
	return filepath.Join(root, "config.json")
}

// Load は、設定を読む。ファイルがなければ既定の値を返す。型の合わない値は、黙って既定にせずエラーにする。
func Load(root string) (Settings, error) {
	s, _, err := load(root)
	return s, err
}

// Set は、1 つの設定を変える。知らない項目は、新しいバージョンの vMeltype が書いたものかもしれないので消さない。
func Set(root, name, value string) error {
	k, ok := keys[name]
	if !ok {
		return fmt.Errorf("%w: %q (設定できるもの: %s)", ErrInvalid, name, strings.Join(keyNames(), "、"))
	}
	parsed, ok := k.parse(value)
	if !ok {
		return fmt.Errorf("%w: %s には %s を指定してください: %q", ErrInvalid, name, k.hint, value)
	}
	raw, err := readRaw(root)
	if err != nil {
		return err
	}
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}
	raw[name], _ = json.Marshal(parsed)
	return writeRaw(root, raw)
}

// Items は、すべての設定の、いまの値と既定の値を返す。
func Items(root string) ([]Item, error) {
	_, values, err := load(root)
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, name := range keyNames() {
		k := keys[name]
		items = append(items, Item{Key: name, Value: values[name], Default: k.defaultValue, Description: k.description})
	}
	return items, nil
}

// load は、既定の値にファイルの値を重ねた設定と、項目ごとの表示する値を返す。
func load(root string) (Settings, map[string]string, error) {
	raw, err := readRaw(root)
	if err != nil {
		return Settings{}, nil, err
	}
	var s Settings
	values := map[string]string{}
	for _, name := range keyNames() {
		k := keys[name]
		data, ok := raw[name]
		if !ok {
			data = json.RawMessage(k.defaultValue)
		}
		value, err := k.apply(&s, data)
		if err != nil {
			return Settings{}, nil, fmt.Errorf("設定 %s の値を読めません (%s)。\"vmeltype app config set %s %s\" などで直してください", name, data, name, k.defaultValue)
		}
		values[name] = value
	}
	return s, values, nil
}

func keyNames() []string {
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func readRaw(root string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("設定を読めませんでした: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("設定 (%s) を読めません: %w", path(root), err)
	}
	return raw, nil
}

// writeRaw は、同じディレクトリの一時ファイルへ書いてから置き換え、書きかけの設定を残さない。
func writeRaw(root string, raw map[string]json.RawMessage) error {
	if err := privatefs.MkdirAll(root); err != nil {
		return err
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, "config.json.*.tmp")
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
	return os.Rename(tmp.Name(), path(root))
}
