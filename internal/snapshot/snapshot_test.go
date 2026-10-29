package snapshot

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if d.Type().IsRegular() {
			rel, _ := filepath.Rel(dir, path)
			data, _ := os.ReadFile(path)
			got[filepath.ToSlash(rel)] = string(data)
		}
		return nil
	})
	return got
}

func TestCreateSavesAllRegularFiles(t *testing.T) {
	root, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
	writeTree(t, data, map[string]string{
		"config.json":                 "{}",
		"dictionaries/japanese.txt":   "辞書",
		"mozc/user_history.db":        "学習",
		"unknown-from-new-version.xy": "未知",
	})
	os.MkdirAll(filepath.Join(data, "empty"), 0o700)

	m, err := Create(root, data, Meta{Reason: ReasonManual, ActiveTag: "v1.4.0"}, now)
	if err != nil {
		t.Fatal(err)
	}

	if len(m.Files) != 4 || m.TotalBytes != int64(len("{}辞書学習未知")) {
		t.Errorf("保存したファイルが違う: %d 件、%d バイト", len(m.Files), m.TotalBytes)
	}
	if m.ActiveTag != "v1.4.0" || m.Reason != ReasonManual || !m.CreatedAt.Equal(now) {
		t.Errorf("manifest の情報が違う: %+v", m)
	}
	listed, problems, err := List(root)
	if err != nil || len(problems) != 0 || len(listed) != 1 || listed[0].ID != m.ID {
		t.Errorf("一覧に出ない: %+v, %v, %v", listed, problems, err)
	}
	if err := Verify(root, m.ID); err != nil {
		t.Errorf("作った直後の検査に失敗した: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "snapshots")); len(entries) != 1 {
		t.Errorf("作業用のディレクトリが残っている: %v", entries)
	}
}

func TestCreateWithoutData(t *testing.T) {
	_, err := Create(t.TempDir(), filepath.Join(t.TempDir(), "data"), Meta{Reason: ReasonManual}, now)

	if !errors.Is(err, ErrNoData) {
		t.Errorf("err = %v, want ErrNoData", err)
	}
}

func TestCreateRejectsLinks(t *testing.T) {
	root, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
	writeTree(t, data, map[string]string{"config.json": "{}"})
	if err := os.Symlink(filepath.Join(data, "config.json"), filepath.Join(data, "link.json")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("シンボリックリンクを作る権限がない")
		}
		t.Fatal(err)
	}

	_, err := Create(root, data, Meta{Reason: ReasonManual}, now)

	if !errors.Is(err, ErrUnsupportedEntry) {
		t.Errorf("err = %v, want ErrUnsupportedEntry", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "snapshots")); len(entries) != 0 {
		t.Errorf("途中のスナップショットが残っている: %v", entries)
	}
}

func TestCreateGivesDistinctIDsAtSameTime(t *testing.T) {
	root, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
	writeTree(t, data, map[string]string{"a": "1"})

	first, err1 := Create(root, data, Meta{Reason: ReasonManual}, now)
	second, err2 := Create(root, data, Meta{Reason: ReasonManual}, now)

	if err1 != nil || err2 != nil || first.ID == second.ID {
		t.Errorf("同じ時刻のスナップショットの ID が重なった: %q, %q (%v, %v)", first.ID, second.ID, err1, err2)
	}
}

func TestVerifyDetectsChanges(t *testing.T) {
	root, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
	writeTree(t, data, map[string]string{"config.json": "{}", "userdict.txt": "辞書"})
	m, _ := Create(root, data, Meta{Reason: ReasonManual}, now)
	files := filepath.Join(root, "snapshots", m.ID, "files")

	t.Run("中身の書き換え", func(t *testing.T) {
		os.WriteFile(filepath.Join(files, "config.json"), []byte("{x}"), 0o600)
		if err := Verify(root, m.ID); !errors.Is(err, ErrCorrupted) {
			t.Errorf("err = %v, want ErrCorrupted", err)
		}
		os.WriteFile(filepath.Join(files, "config.json"), []byte("{}"), 0o600)
	})
	t.Run("ファイルの欠け", func(t *testing.T) {
		os.Remove(filepath.Join(files, "userdict.txt"))
		if err := Verify(root, m.ID); !errors.Is(err, ErrCorrupted) {
			t.Errorf("err = %v, want ErrCorrupted", err)
		}
	})
}

func TestRestoreReplacesData(t *testing.T) {
	root, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
	writeTree(t, data, map[string]string{"config.json": "古い設定", "dictionaries/japanese.txt": "古い辞書"})
	os.MkdirAll(filepath.Join(data, "empty"), 0o700)
	m, _ := Create(root, data, Meta{Reason: ReasonManual}, now)
	os.RemoveAll(data)
	writeTree(t, data, map[string]string{"config.json": "新しい設定", "added-later.txt": "後から"})

	if err := Restore(root, m.ID, data); err != nil {
		t.Fatal(err)
	}

	got := readTree(t, data)
	if len(got) != 2 || got["config.json"] != "古い設定" || got["dictionaries/japanese.txt"] != "古い辞書" {
		t.Errorf("スナップショットの時点と違う: %v", got)
	}
	if info, err := os.Stat(filepath.Join(data, "empty")); err != nil || !info.IsDir() {
		t.Errorf("空のディレクトリを戻していない")
	}
	if entries, _ := os.ReadDir(filepath.Dir(data)); len(entries) != 1 {
		t.Errorf("作業用や退避のディレクトリが残っている: %v", entries)
	}
}

func TestRestoreRefusesCorruptedSnapshot(t *testing.T) {
	root, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
	writeTree(t, data, map[string]string{"config.json": "保存した設定"})
	m, _ := Create(root, data, Meta{Reason: ReasonManual}, now)
	os.WriteFile(filepath.Join(root, "snapshots", m.ID, "files", "config.json"), []byte("壊れた"), 0o600)
	writeTree(t, data, map[string]string{"config.json": "いまの設定"})

	err := Restore(root, m.ID, data)

	if !errors.Is(err, ErrCorrupted) {
		t.Errorf("err = %v, want ErrCorrupted", err)
	}
	if got := readTree(t, data)["config.json"]; got != "いまの設定" {
		t.Errorf("壊れたスナップショットでいまのデータを変えた: %q", got)
	}
}

func TestListReportsBrokenManifest(t *testing.T) {
	root, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
	writeTree(t, data, map[string]string{"a": "1"})
	Create(root, data, Meta{Reason: ReasonManual}, now)
	broken := filepath.Join(root, "snapshots", "20260101T000000Z")
	os.MkdirAll(broken, 0o700)
	os.WriteFile(filepath.Join(broken, "manifest.json"), []byte("{"), 0o600)

	listed, problems, err := List(root)

	if err != nil || len(listed) != 1 || len(problems) != 1 {
		t.Errorf("listed = %d, problems = %v, err = %v", len(listed), problems, err)
	}
}

func TestGetUnknownID(t *testing.T) {
	if _, err := Get(t.TempDir(), "20260101T000000Z"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, err := Get(t.TempDir(), `..\..\evil`); !errors.Is(err, ErrNotFound) {
		t.Errorf("パスを含む ID を受け付けた: %v", err)
	}
}

func TestInterruptedRestoreIsRecovered(t *testing.T) {
	t.Run("置き換える前に止まっていれば、退避したデータを戻す", func(t *testing.T) {
		root, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
		aside := filepath.Join(filepath.Dir(data), ".data-replaced-1", "data")
		writeTree(t, aside, map[string]string{"config.json": "退避した設定"})
		writeTree(t, filepath.Join(filepath.Dir(data), ".data-restore-1"), map[string]string{"config.json": "組み立て途中"})

		m, err := Create(root, data, Meta{Reason: ReasonManual}, now)

		if err != nil || len(m.Files) != 1 || readTree(t, data)["config.json"] != "退避した設定" {
			t.Errorf("退避したデータを戻していない: %v, %v", err, readTree(t, data))
		}
		if entries, _ := os.ReadDir(filepath.Dir(data)); len(entries) != 1 {
			t.Errorf("跡が残っている: %v", entries)
		}
	})
	t.Run("置き換えのあとで止まっていれば、退避したものを消す", func(t *testing.T) {
		root, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
		writeTree(t, data, map[string]string{"config.json": "復元した設定"})
		writeTree(t, filepath.Join(filepath.Dir(data), ".data-replaced-1", "data"), map[string]string{"config.json": "前の設定"})

		if _, err := Create(root, data, Meta{Reason: ReasonManual}, now); err != nil {
			t.Fatal(err)
		}

		if readTree(t, data)["config.json"] != "復元した設定" {
			t.Errorf("復元したデータを変えた")
		}
		if entries, _ := os.ReadDir(filepath.Dir(data)); len(entries) != 1 {
			t.Errorf("跡が残っている: %v", entries)
		}
	})
}
