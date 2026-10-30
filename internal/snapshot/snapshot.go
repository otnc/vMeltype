// Package snapshot は、Meltype のデータを保存、検査、復元する。通信しない。
// 本家のバックアップは決まったファイルしか含まないので、未知のものも含めて data の通常のファイルをすべて保存する。
package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/otnc/meltool/internal/archive"
	"github.com/otnc/meltool/internal/privatefs"
)

// SchemaVersion は、このバージョンの Meltool が読み書きする manifest の形式のバージョン。
const SchemaVersion = 1

var (
	// ErrNoData は、保存するデータのディレクトリがないことを表す。
	ErrNoData = errors.New("保存するデータがありません")
	// ErrUnsupportedEntry は、リンクなど、通常のファイルとして保存できないものがあることを表す。
	ErrUnsupportedEntry = errors.New("通常のファイルでもディレクトリでもないものがあるので、完全には保存できません")
	// ErrNotFound は、指定したスナップショットがないことを表す。
	ErrNotFound = errors.New("スナップショットがありません")
	// ErrCorrupted は、スナップショットの中身が manifest と一致しないことを表す。
	ErrCorrupted   = errors.New("スナップショットの中身が記録と一致しません")
	errNewerSchema = errors.New("新しいバージョンの Meltool が作ったスナップショットなので読めません。Meltool を更新してください")
)

// Reason は、スナップショットを作った理由。
type Reason string

const (
	ReasonManual        Reason = "manual"
	ReasonBeforeRestore Reason = "before-restore"
	// ReasonBeforeUse は、通常版の use が、バージョンを切り替える前に自動で作ったもの。
	ReasonBeforeUse Reason = "before-use"
)

// Meta は、作るときに記録する付加情報。
type Meta struct {
	Reason    Reason
	ActiveTag string
}

// Manifest は、1 つのスナップショットの記録。Files のパスは / 区切りで、data からの相対。
type Manifest struct {
	SchemaVersion int            `json:"schemaVersion"`
	ID            string         `json:"id"`
	CreatedAt     time.Time      `json:"createdAt"`
	Reason        Reason         `json:"reason"`
	ActiveTag     string         `json:"activeTag,omitempty"`
	Source        string         `json:"source"`
	Dirs          []string       `json:"dirs"`
	Files         []archive.File `json:"files"`
	TotalBytes    int64          `json:"totalBytes"`
	// ImportedAt は、書き出したファイルから取り込んだ日時。この管理ルートで作ったものは nil。
	ImportedAt *time.Time `json:"importedAt,omitempty"`
}

// Problem は、読めなかったスナップショット。
type Problem struct {
	ID  string
	Err error
}

var idPattern = regexp.MustCompile(`^\d{8}T\d{6}Z(-\d+)?$`)

func snapshotsDir(root string) string {
	return filepath.Join(root, "snapshots")
}

// Path は、スナップショットのディレクトリを返す。
func Path(root, id string) string {
	return filepath.Join(snapshotsDir(root), id)
}

// Create は、dataDir の中身を保存する。作業用のディレクトリで完成させてから置くので、途中で止まっても一覧に出ない。
// 実行中の Meltype が書き込んでいる最中のデータを保存しないよう、呼び出し側が Meltype の停止を確かめておく。
func Create(root, dataDir string, meta Meta, now time.Time) (Manifest, error) {
	if err := recoverInterruptedRestore(dataDir); err != nil {
		return Manifest{}, err
	}
	info, err := os.Stat(dataDir)
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, fmt.Errorf("%w (%s)", ErrNoData, dataDir)
	}
	if err != nil {
		return Manifest{}, err
	}
	if !info.IsDir() {
		return Manifest{}, fmt.Errorf("%s はディレクトリではありません", dataDir)
	}

	if err := privatefs.MkdirAll(snapshotsDir(root)); err != nil {
		return Manifest{}, err
	}
	staging, err := os.MkdirTemp(snapshotsDir(root), ".staging-*")
	if err != nil {
		return Manifest{}, err
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(staging)
		}
	}()

	m := Manifest{SchemaVersion: SchemaVersion, CreatedAt: now.UTC(), Reason: meta.Reason, ActiveTag: meta.ActiveTag, Source: dataDir, Dirs: []string{}, Files: []archive.File{}}
	filesDir := filepath.Join(staging, "files")
	err = filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.Mkdir(filesDir, 0o700)
		}
		slashed := filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			m.Dirs = append(m.Dirs, slashed)
			return os.Mkdir(filepath.Join(filesDir, rel), 0o700)
		case d.Type().IsRegular():
			file, err := copyFile(path, filepath.Join(filesDir, rel))
			if err != nil {
				return err
			}
			file.Path = slashed
			m.Files = append(m.Files, file)
			m.TotalBytes += file.Size
			return nil
		default:
			return fmt.Errorf("%w: %s", ErrUnsupportedEntry, path)
		}
	})
	if err != nil {
		return Manifest{}, err
	}

	for attempt := 1; ; attempt++ {
		m.ID = now.UTC().Format("20060102T150405Z")
		if attempt > 1 {
			m.ID = fmt.Sprintf("%s-%d", m.ID, attempt)
		}
		if _, err := os.Stat(Path(root, m.ID)); err == nil {
			continue
		}
		if err := writeManifest(filepath.Join(staging, "manifest.json"), m); err != nil {
			return Manifest{}, err
		}
		if err := os.Rename(staging, Path(root, m.ID)); err != nil {
			if attempt < 100 && errors.Is(err, fs.ErrExist) {
				continue
			}
			return Manifest{}, fmt.Errorf("スナップショットを置けませんでした: %w", err)
		}
		committed = true
		return m, nil
	}
}

// Get は、スナップショットの manifest を読む。
func Get(root, id string) (Manifest, error) {
	if !idPattern.MatchString(id) {
		return Manifest{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	data, err := os.ReadFile(filepath.Join(Path(root, id), "manifest.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("スナップショット %s の manifest を読めません: %w", id, err)
	}
	if m.SchemaVersion > SchemaVersion {
		return Manifest{}, errNewerSchema
	}
	if m.ID != id {
		return Manifest{}, fmt.Errorf("スナップショット %s の manifest が別の ID (%s) を示しています", id, m.ID)
	}
	return m, nil
}

// List は、スナップショットを新しい順に返す。読めないものは problems に入れ、一覧の表示は止めない。
func List(root string) ([]Manifest, []Problem, error) {
	entries, err := os.ReadDir(snapshotsDir(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var listed []Manifest
	var problems []Problem
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		m, err := Get(root, e.Name())
		if err != nil {
			problems = append(problems, Problem{ID: e.Name(), Err: err})
			continue
		}
		listed = append(listed, m)
	}
	slices.SortFunc(listed, func(a, b Manifest) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return listed, problems, nil
}

// Verify は、スナップショットの各ファイルを manifest のサイズと SHA-256 と照合する。
func Verify(root, id string) error {
	m, err := Get(root, id)
	if err != nil {
		return err
	}
	filesDir := filepath.Join(Path(root, id), "files")
	for _, f := range m.Files {
		got, err := hashFile(filepath.Join(filesDir, filepath.FromSlash(f.Path)))
		if err != nil {
			return fmt.Errorf("%w: %s を読めません (%v)", ErrCorrupted, f.Path, err)
		}
		if got.Size != f.Size || got.SHA256 != f.SHA256 {
			return fmt.Errorf("%w: %s", ErrCorrupted, f.Path)
		}
	}
	return nil
}

// Restore は、dataDir をスナップショットの時点の中身に置き換える。先に照合し、組み立ててから rename で入れ替える。
// いまのデータは消えるので、呼び出し側が先に別のスナップショットとして保存しておく。
func Restore(root, id, dataDir string) error {
	if err := Verify(root, id); err != nil {
		return err
	}
	m, err := Get(root, id)
	if err != nil {
		return err
	}
	if err := recoverInterruptedRestore(dataDir); err != nil {
		return err
	}

	parent := filepath.Dir(dataDir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".data-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	for _, dir := range m.Dirs {
		if err := os.MkdirAll(filepath.Join(staging, filepath.FromSlash(dir)), 0o700); err != nil {
			return err
		}
	}
	filesDir := filepath.Join(Path(root, id), "files")
	for _, f := range m.Files {
		dst := filepath.Join(staging, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		copied, err := copyFile(filepath.Join(filesDir, filepath.FromSlash(f.Path)), dst)
		if err != nil {
			return err
		}
		if copied.SHA256 != f.SHA256 {
			return fmt.Errorf("%w: %s", ErrCorrupted, f.Path)
		}
	}

	if _, err := os.Stat(dataDir); errors.Is(err, fs.ErrNotExist) {
		return os.Rename(staging, dataDir)
	}
	replaced, err := os.MkdirTemp(parent, ".data-replaced-*")
	if err != nil {
		return err
	}
	aside := filepath.Join(replaced, filepath.Base(dataDir))
	if err := os.Rename(dataDir, aside); err != nil {
		os.Remove(replaced)
		return fmt.Errorf("いまのデータを退避できませんでした。Meltype が使っていないか確かめてください: %w", err)
	}
	if err := os.Rename(staging, dataDir); err != nil {
		if backErr := os.Rename(aside, dataDir); backErr != nil {
			return fmt.Errorf("データを置き換えられず、元に戻すこともできませんでした。元のデータは %s にあります: %w", aside, errors.Join(err, backErr))
		}
		os.Remove(replaced)
		return fmt.Errorf("データを置き換えられませんでした (元のデータに戻しました): %w", err)
	}
	return os.RemoveAll(replaced)
}

// recoverInterruptedRestore は、復元の途中で止まった跡を片付ける。
// data がなく、退避したデータが 1 つだけ残っていれば、置き換える前に止まったので戻す。
// data があれば、置き換えのあとの掃除の前に止まったので、退避したものを消す。
func recoverInterruptedRestore(dataDir string) error {
	parent := filepath.Dir(dataDir)
	entries, err := os.ReadDir(parent)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var replaced []string
	for _, e := range entries {
		switch {
		case strings.HasPrefix(e.Name(), ".data-restore-"):
			if err := os.RemoveAll(filepath.Join(parent, e.Name())); err != nil {
				return err
			}
		case strings.HasPrefix(e.Name(), ".data-replaced-"):
			replaced = append(replaced, filepath.Join(parent, e.Name()))
		}
	}
	if len(replaced) == 0 {
		return nil
	}
	_, statErr := os.Stat(dataDir)
	dataExists := statErr == nil
	if !dataExists && len(replaced) == 1 {
		aside := filepath.Join(replaced[0], filepath.Base(dataDir))
		if err := os.Rename(aside, dataDir); err != nil {
			return fmt.Errorf("中断した復元の前のデータ (%s) を戻せませんでした: %w", aside, err)
		}
		return os.Remove(replaced[0])
	}
	if !dataExists {
		return fmt.Errorf("中断した復元の退避先が複数あり、どれを戻すか決められません。%s の中を確かめてください", parent)
	}
	for _, r := range replaced {
		if err := os.RemoveAll(r); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) (archive.File, error) {
	in, err := os.Open(src)
	if err != nil {
		return archive.File{}, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return archive.File{}, err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, hash), in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return archive.File{}, fmt.Errorf("%s をコピーできませんでした: %w", src, err)
	}
	return archive.File{Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func hashFile(path string) (archive.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return archive.File{}, err
	}
	defer f.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, f)
	if err != nil {
		return archive.File{}, err
	}
	return archive.File{Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func writeManifest(path string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return privatefs.WriteFile(path, data)
}
