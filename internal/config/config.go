// Package config は、管理ルートに置く Meltool の設定を読み書きする。通信しない。
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

	"github.com/otnc/meltool/internal/privatefs"
)

// ErrInvalid は、Set に渡した項目か値を受け付けないことを表す。
var ErrInvalid = errors.New("設定できません")

// Settings は、Meltool が使う設定。ファイルにない項目は既定の値になる。
type Settings struct {
	// StopAppOnYes は、-y だけで、動いている実行先の Meltype を終了させるか。
	StopAppOnYes bool
}

// Item は、show で表示する 1 つの設定。
type Item struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

type boolKey struct {
	defaultValue bool
	description  string
	field        func(*Settings) *bool
}

var keys = map[string]boolKey{
	"stop-app-on-yes": {
		defaultValue: true,
		description:  "-y だけで、動いている実行先の Meltype を終了させる。false なら --stop-app を付けたときだけ終了させる",
		field:        func(s *Settings) *bool { return &s.StopAppOnYes },
	},
}

func path(root string) string {
	return filepath.Join(root, "config.json")
}

// Load は、設定を読む。ファイルがなければ既定の値を返す。型の合わない値は、黙って既定にせずエラーにする。
func Load(root string) (Settings, error) {
	raw, err := readRaw(root)
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	for name, k := range keys {
		value, err := k.read(raw, name)
		if err != nil {
			return Settings{}, err
		}
		*k.field(&s) = value
	}
	return s, nil
}

// Set は、1 つの設定を変える。知らない項目は、新しいバージョンの Meltool が書いたものかもしれないので消さない。
func Set(root, key, value string) error {
	if _, ok := keys[key]; !ok {
		return fmt.Errorf("%w: %q (設定できるもの: %s)", ErrInvalid, key, strings.Join(keyNames(), "、"))
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("%w: %s には true か false を指定してください: %q", ErrInvalid, key, value)
	}
	raw, err := readRaw(root)
	if err != nil {
		return err
	}
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}
	raw[key], _ = json.Marshal(parsed)
	return writeRaw(root, raw)
}

// Items は、すべての設定の、いまの値と既定の値を返す。
func Items(root string) ([]Item, error) {
	raw, err := readRaw(root)
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, name := range keyNames() {
		k := keys[name]
		value, err := k.read(raw, name)
		if err != nil {
			return nil, err
		}
		items = append(items, Item{Key: name, Value: strconv.FormatBool(value), Default: strconv.FormatBool(k.defaultValue), Description: k.description})
	}
	return items, nil
}

func (k boolKey) read(raw map[string]json.RawMessage, name string) (bool, error) {
	data, ok := raw[name]
	if !ok {
		return k.defaultValue, nil
	}
	var value bool
	if err := json.Unmarshal(data, &value); err != nil {
		return false, fmt.Errorf("設定 %s の値を読めません (%s)。\"meltool app config set %s true\" などで直してください", name, data, name)
	}
	return value, nil
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
