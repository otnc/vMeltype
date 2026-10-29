// Package store は、管理ルートの versions に置く保持版と、その manifest を扱う。通信しない。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/otnc/vmeltype/internal/archive"
	"github.com/otnc/vmeltype/internal/privatefs"
	"github.com/otnc/vmeltype/internal/versions"
)

// SchemaVersion は、この版の vMeltype が読み書きする manifest の形式の版。
const SchemaVersion = 1

const manifestFile = "manifest.json"

// 作業中や置き換え中のディレクトリは . で始め、保持版として扱わない。
const (
	stagingPrefix  = ".staging-"
	replacedPrefix = ".replaced-"
)

// ExpectedFromGitHubDigest は、アーカイブの期待値が GitHub の API の digest から来たことを表す。
const ExpectedFromGitHubDigest = "github-digest"

var ErrNotStored = errors.New("その版は保持していません")

// Stored は、1 つの保持版の manifest。
type Stored struct {
	SchemaVersion int            `json:"schemaVersion"`
	ID            string         `json:"id"`
	Tag           string         `json:"tag"`
	Prerelease    bool           `json:"prerelease"`
	OS            string         `json:"os"`
	Arch          string         `json:"arch"`
	Variant       string         `json:"variant"`
	Asset         Asset          `json:"asset"`
	Archive       ArchiveRecord  `json:"archive"`
	Files         []archive.File `json:"files"`
	StoredAt      time.Time      `json:"storedAt"`
}

// Asset は、取得した配布物の、公式一覧での情報。
type Asset struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"downloadUrl"`
}

// ArchiveRecord は、保持したアーカイブの検査値と、照合した期待値の出典。
type ArchiveRecord struct {
	File           string `json:"file"`
	SHA256         string `json:"sha256"`
	Expected       string `json:"expected,omitempty"`
	ExpectedSource string `json:"expectedSource,omitempty"`
}

// Problem は、manifest を読めない保持版。
type Problem struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

var unsafeIDChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// ID は、タグ、OS、CPU、variant から保持 ID を作る。タグから実パスを推測しないよう、
// ディレクトリ名に使えない文字は置き換える。
func ID(tag, goos, arch, variant string) string {
	safeTag := strings.Trim(unsafeIDChars.ReplaceAllString(tag, "_"), ".")
	return fmt.Sprintf("%s_%s-%s_%s", safeTag, goos, arch, variant)
}

// Store は、管理ルートの保持版の置き場。
type Store struct {
	Root string
}

func (s Store) versionsDir() string {
	return filepath.Join(s.Root, "versions")
}

// Path は、保持版のディレクトリを返す。
func (s Store) Path(id string) string {
	return filepath.Join(s.versionsDir(), id)
}

// List は、保持版を SemVer の新しい順に返す。manifest を読めないものは problems に入れ、ほかの保持版は読み続ける。
func (s Store) List() (stored []Stored, problems []Problem, err error) {
	entries, err := os.ReadDir(s.versionsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		m, err := s.Get(e.Name())
		if err != nil {
			problems = append(problems, Problem{ID: e.Name(), Reason: err.Error()})
			continue
		}
		stored = append(stored, m)
	}
	slices.SortStableFunc(stored, func(a, b Stored) int { return versions.Compare(b.Tag, a.Tag) })
	return stored, problems, nil
}

// Get は、保持 ID の manifest を読む。
func (s Store) Get(id string) (Stored, error) {
	if id != filepath.Base(id) || strings.HasPrefix(id, ".") {
		return Stored{}, fmt.Errorf("%w: %s", ErrNotStored, id)
	}
	data, err := os.ReadFile(filepath.Join(s.Path(id), manifestFile))
	if errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Stat(s.Path(id)); statErr == nil {
			return Stored{}, errors.New("manifest がありません")
		}
		return Stored{}, fmt.Errorf("%w: %s", ErrNotStored, id)
	}
	if err != nil {
		return Stored{}, err
	}
	var m Stored
	if err := json.Unmarshal(data, &m); err != nil {
		return Stored{}, fmt.Errorf("manifest が壊れています: %w", err)
	}
	if m.SchemaVersion > SchemaVersion {
		return Stored{}, errors.New("新しい版の vMeltype が保存した manifest なので読めません")
	}
	if m.ID != id {
		return Stored{}, fmt.Errorf("manifest の保持 ID (%s) がディレクトリ名と一致しません", m.ID)
	}
	return m, nil
}

// Staging は、作業中の保持版。Commit するまで一覧に出ない。
type Staging struct {
	store Store
	dir   string
}

// Stage は、versions の中に作業用のディレクトリを作る。置き換えを rename で済ませるため、同じボリュームに作る。
func (s Store) Stage() (*Staging, error) {
	if err := privatefs.MkdirAll(s.versionsDir()); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(s.versionsDir(), stagingPrefix+"*")
	if err != nil {
		return nil, err
	}
	return &Staging{store: s, dir: dir}, nil
}

// Dir は、作業用のディレクトリを返す。
func (st *Staging) Dir() string {
	return st.dir
}

// Commit は、manifest を書いてから作業用のディレクトリを保持版の場所へ移す。
// 同じ保持 ID があれば、退避してから置き換え、置き換えに失敗したら元へ戻す。
func (st *Staging) Commit(m Stored) error {
	m.SchemaVersion = SchemaVersion
	data, err := json.MarshalIndent(&m, "", "  ")
	if err != nil {
		return err
	}
	if err := privatefs.WriteFile(filepath.Join(st.dir, manifestFile), data); err != nil {
		return err
	}

	target := st.store.Path(m.ID)
	var replaced string
	if _, err := os.Stat(target); err == nil {
		replaced = filepath.Join(st.store.versionsDir(), replacedPrefix+filepath.Base(st.dir)[len(stagingPrefix):])
		if err := os.Rename(target, replaced); err != nil {
			return fmt.Errorf("前の保持版を退避できませんでした: %w", err)
		}
	}
	if err := os.Rename(st.dir, target); err != nil {
		if replaced != "" {
			if restoreErr := os.Rename(replaced, target); restoreErr != nil {
				return fmt.Errorf("保持版を置けず、前の保持版も戻せませんでした (%s に残っています): %w", replaced, errors.Join(err, restoreErr))
			}
		}
		return fmt.Errorf("保持版を置けませんでした: %w", err)
	}
	if replaced != "" {
		if err := os.RemoveAll(replaced); err != nil {
			return fmt.Errorf("保持版は置けましたが、前の保持版 (%s) を消せませんでした: %w", replaced, err)
		}
	}
	return nil
}

// Discard は、作業用のディレクトリを消す。
func (st *Staging) Discard() error {
	return os.RemoveAll(st.dir)
}
