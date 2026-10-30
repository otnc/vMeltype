package snapshot

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// exported は、データを保存して書き出したファイルを返す。データには空のディレクトリも入れる。
func exported(t *testing.T) (file string, m Manifest) {
	t.Helper()
	root := t.TempDir()
	data := filepath.Join(t.TempDir(), "data")
	writeTree(t, data, map[string]string{"userdict.txt": "辞書", "mozc/segment.db": "学習"})
	os.MkdirAll(filepath.Join(data, "dictionaries"), 0o700)
	m, err := Create(root, data, Meta{Reason: ReasonManual, ActiveTag: "v1.4.0"}, now)
	if err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(t.TempDir(), "data.vmeltype-snapshot")
	if err := Export(root, m.ID, file); err != nil {
		t.Fatal(err)
	}
	return file, m
}

func TestExportImport(t *testing.T) {
	t.Run("別の管理ルートに取り込み、そのまま復元できる", func(t *testing.T) {
		file, original := exported(t)
		other := t.TempDir()

		imported, err := Import(other, file, now)
		if err != nil {
			t.Fatal(err)
		}
		if imported.ID != original.ID || imported.ImportedAt == nil || imported.Source != original.Source {
			t.Errorf("取り込んだ記録が違う: %+v", imported)
		}
		if err := Verify(other, imported.ID); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(t.TempDir(), "restored")
		if err := Restore(other, imported.ID, dest); err != nil {
			t.Fatal(err)
		}
		if got := readTree(t, dest); got["userdict.txt"] != "辞書" || got["mozc/segment.db"] != "学習" {
			t.Errorf("復元した中身が違う: %v", got)
		}
		if _, err := os.Stat(filepath.Join(dest, "dictionaries")); err != nil {
			t.Errorf("空のディレクトリを戻していない")
		}
	})
	t.Run("同じ ID があれば、別の ID で取り込む", func(t *testing.T) {
		file, original := exported(t)
		other := t.TempDir()
		Import(other, file, now)

		second, err := Import(other, file, now)

		if err != nil || second.ID != original.ID+"-2" {
			t.Errorf("ID = %q, err = %v", second.ID, err)
		}
	})
	t.Run("既にあるファイルには書き出さない", func(t *testing.T) {
		file, original := exported(t)
		err := Export(t.TempDir(), original.ID, file)

		if err == nil {
			t.Error("既にあるファイルに書き出した")
		}
	})
}

// rewrite は、書き出したファイルを読み、change で書き換えた ZIP を作る。
func rewrite(t *testing.T, file string, change func(name string, body []byte) (string, []byte, bool), extra map[string]string) string {
	t.Helper()
	r, err := zip.OpenReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out := filepath.Join(t.TempDir(), "changed.vmeltype-snapshot")
	f, _ := os.Create(out)
	w := zip.NewWriter(f)
	for _, entry := range r.File {
		rc, _ := entry.Open()
		body, _ := io.ReadAll(rc)
		rc.Close()
		name, body, keep := change(entry.Name, body)
		if !keep {
			continue
		}
		fw, _ := w.Create(name)
		fw.Write(body)
	}
	for name, body := range extra {
		fw, _ := w.Create(name)
		fw.Write([]byte(body))
	}
	w.Close()
	f.Close()
	return out
}

func TestImportRejects(t *testing.T) {
	keep := func(name string, body []byte) (string, []byte, bool) { return name, body, true }
	tests := []struct {
		name   string
		change func(string, []byte) (string, []byte, bool)
		extra  map[string]string
	}{
		{
			name: "中身が manifest と違う",
			change: func(name string, body []byte) (string, []byte, bool) {
				if name == "files/userdict.txt" {
					return name, []byte("書き換えた"), true
				}
				return name, body, true
			},
		},
		{
			name: "manifest にあるファイルがない",
			change: func(name string, body []byte) (string, []byte, bool) {
				return name, body, name != "files/mozc/segment.db"
			},
		},
		{name: "manifest にないファイルがある", change: keep, extra: map[string]string{"files/extra.txt": "余計"}},
		{name: "manifest の外にファイルがある", change: keep, extra: map[string]string{"other.txt": "余計"}},
		{
			name: "manifest がない",
			change: func(name string, body []byte) (string, []byte, bool) {
				return name, body, name != "manifest.json"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name+"なら取り込まない", func(t *testing.T) {
			file, _ := exported(t)
			changed := rewrite(t, file, tt.change, tt.extra)
			other := t.TempDir()

			_, err := Import(other, changed, now)

			if !errors.Is(err, ErrCorrupted) {
				t.Errorf("err = %v", err)
			}
			if listed, problems, _ := List(other); len(listed) != 0 || len(problems) != 0 {
				t.Errorf("取り込まないはずが残した: %v %v", listed, problems)
			}
			if entries, _ := os.ReadDir(filepath.Join(other, "snapshots")); len(entries) != 0 {
				t.Errorf("作業用のディレクトリが残っている: %v", entries)
			}
		})
	}
}
