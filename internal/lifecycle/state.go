// Package lifecycle は、保持バージョンを有効にする処理と、その記録 (有効なバージョン、処理記録、排他) を扱う。通信しない。
package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/otnc/vmeltype/internal/privatefs"
)

// SchemaVersion は、このバージョンの vMeltype が読み書きする状態と処理記録の形式のバージョン。
const SchemaVersion = 1

var errNewerSchema = errors.New("新しいバージョンの vMeltype が保存した形式なので読めません。vMeltype を更新してください")

// Activation は、有効にした 1 つのバージョン。
type Activation struct {
	StoreID string `json:"storeId"`
	Tag     string `json:"tag"`
	Variant string `json:"variant"`
	AppDir  string `json:"appDir"`
	// OwnedEntries は、実行先の直下に vMeltype が置いた名前。次の切り替えで、これ以外 (data など) を引き継ぐ。
	OwnedEntries []string  `json:"ownedEntries"`
	ActivatedAt  time.Time `json:"activatedAt"`
}

// State は、有効なバージョンと直前のバージョン、固定の記録。
type State struct {
	SchemaVersion int         `json:"schemaVersion"`
	Active        *Activation `json:"active,omitempty"`
	Previous      *Activation `json:"previous,omitempty"`
	Pin           *Pinned     `json:"pin,omitempty"`
}

func stateDir(root string) string {
	return filepath.Join(root, "state")
}

// LoadState は、有効なバージョンの記録を読む。まだ何も有効にしていなければ空の記録を返す。
func LoadState(root string) (State, error) {
	var s State
	if err := readJSON(filepath.Join(stateDir(root), "active.json"), &s); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("有効なバージョンの記録を読めませんでした: %w", err)
	}
	if s.SchemaVersion > SchemaVersion {
		return State{}, errNewerSchema
	}
	return s, nil
}

func saveState(root string, s State) error {
	s.SchemaVersion = SchemaVersion
	return writeJSON(filepath.Join(stateDir(root), "active.json"), &s)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// writeJSON は、同じディレクトリの一時ファイルへ書いてから置き換え、書きかけの記録を残さない。
func writeJSON(path string, v any) error {
	if err := privatefs.MkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
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
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
