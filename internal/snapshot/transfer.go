package snapshot

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/otnc/meltool/internal/archive"
	"github.com/otnc/meltool/internal/privatefs"
)

// FileExtension は、書き出したスナップショットのファイルの拡張子。中身は、管理ルートに置くときと同じ形 (manifest.json と files) の ZIP。
const FileExtension = ".meltool-snapshot"

// 書き出したファイルは利用者が持ち運ぶものなので、取り込むときは中身を信用せず、上限を決めて展開する。
var importLimits = archive.Limits{MaxFiles: 100000, MaxTotalBytes: 4 << 30}

var idSuffix = regexp.MustCompile(`-\d+$`)

// Export は、照合したスナップショットを 1 つのファイルに書き出す。既にあるファイルには書き出さない。
// 辞書や学習の中身が入るので、作るときから本人だけが読めるようにする。
func Export(root, id, file string) error {
	if err := Verify(root, id); err != nil {
		return err
	}
	m, err := Get(root, id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(file); err == nil {
		return fmt.Errorf("%s は既にあるので、書き出しません", file)
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), filepath.Base(file)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := writeArchive(tmp, root, m); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}

func writeArchive(w io.Writer, root string, m Manifest) error {
	z := zip.NewWriter(w)
	manifest, err := os.ReadFile(filepath.Join(Path(root, m.ID), "manifest.json"))
	if err != nil {
		return err
	}
	entry, err := z.Create("manifest.json")
	if err != nil {
		return err
	}
	if _, err := entry.Write(manifest); err != nil {
		return err
	}
	for _, dir := range m.Dirs {
		if _, err := z.Create("files/" + dir + "/"); err != nil {
			return err
		}
	}
	filesDir := filepath.Join(Path(root, m.ID), "files")
	for _, f := range m.Files {
		entry, err := z.Create("files/" + f.Path)
		if err != nil {
			return err
		}
		src, err := os.Open(filepath.Join(filesDir, filepath.FromSlash(f.Path)))
		if err != nil {
			return err
		}
		_, err = io.Copy(entry, src)
		src.Close()
		if err != nil {
			return err
		}
	}
	return z.Close()
}

// Import は、書き出したファイルを取り込み、スナップショットとして置く。
// 安全に展開してから、manifest と全ファイルのサイズと SHA-256 が合い、manifest にないものがないときだけ置く。合わなければ何も残さない。
// 同じ ID があれば、末尾に -2 などを付けた ID にする。
func Import(root, file string, now time.Time) (Manifest, error) {
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

	extracted, err := archive.ExtractZip(file, staging, importLimits)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrCorrupted, err)
	}
	m, err := readImportedManifest(staging)
	if err != nil {
		return Manifest{}, err
	}
	if err := matchManifest(m, extracted); err != nil {
		return Manifest{}, err
	}
	for _, dir := range append([]string{""}, m.Dirs...) {
		if dir != "" && !filepath.IsLocal(filepath.FromSlash(dir)) {
			return Manifest{}, fmt.Errorf("%w: ディレクトリの名前 %q を使えません", ErrCorrupted, dir)
		}
		if err := os.MkdirAll(filepath.Join(staging, "files", filepath.FromSlash(dir)), 0o700); err != nil {
			return Manifest{}, err
		}
	}

	importedAt := now.UTC()
	m.ImportedAt = &importedAt
	base := idSuffix.ReplaceAllString(m.ID, "")
	for attempt := 1; ; attempt++ {
		m.ID = base
		if attempt > 1 {
			m.ID = fmt.Sprintf("%s-%d", base, attempt)
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

func readImportedManifest(staging string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(staging, "manifest.json"))
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: manifest.json がありません", ErrCorrupted)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("%w: manifest.json を読めません (%v)", ErrCorrupted, err)
	}
	if m.SchemaVersion > SchemaVersion {
		return Manifest{}, errNewerSchema
	}
	if !idPattern.MatchString(m.ID) {
		return Manifest{}, fmt.Errorf("%w: ID %q を使えません", ErrCorrupted, m.ID)
	}
	return m, nil
}

// matchManifest は、展開したファイルが、manifest.json と manifest の各ファイルにちょうど一致するかを確かめる。
func matchManifest(m Manifest, extracted []archive.File) error {
	want := map[string]archive.File{}
	for _, f := range m.Files {
		want["files/"+f.Path] = f
	}
	seen := 0
	for _, got := range extracted {
		if got.Path == "manifest.json" {
			continue
		}
		f, ok := want[got.Path]
		if !ok {
			return fmt.Errorf("%w: manifest にない %s があります", ErrCorrupted, got.Path)
		}
		if got.Size != f.Size || got.SHA256 != f.SHA256 {
			return fmt.Errorf("%w: %s", ErrCorrupted, f.Path)
		}
		seen++
	}
	if seen != len(want) {
		return fmt.Errorf("%w: manifest にあるファイルが足りません", ErrCorrupted)
	}
	return nil
}
