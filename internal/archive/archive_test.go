package archive

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name string
	body string
	mode fs.FileMode
}

func writeZip(t *testing.T, entries ...entry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for _, e := range entries {
		header := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			header.SetMode(e.mode)
		}
		fw, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

var defaultLimits = Limits{MaxFiles: 100, MaxTotalBytes: 1 << 20}

func TestExtractZipWritesFilesAndRecordsHashes(t *testing.T) {
	archivePath := writeZip(t,
		entry{name: "Meltype/", mode: fs.ModeDir | 0o755},
		entry{name: "Meltype/Meltype.exe", body: "binary"},
		entry{name: "Meltype/dict/user.txt", body: "辞書"},
	)
	dest := filepath.Join(t.TempDir(), "out")

	files, err := ExtractZip(archivePath, dest, defaultLimits)
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "Meltype", "dict", "user.txt"))
	if err != nil || string(got) != "辞書" {
		t.Fatalf("展開したファイルの中身が違う: %q, %v", got, err)
	}
	want := map[string]File{
		"Meltype/Meltype.exe":   {Path: "Meltype/Meltype.exe", Size: 6, SHA256: sha("binary")},
		"Meltype/dict/user.txt": {Path: "Meltype/dict/user.txt", Size: int64(len("辞書")), SHA256: sha("辞書")},
	}
	if len(files) != len(want) {
		t.Fatalf("記録したファイル = %+v", files)
	}
	for _, f := range files {
		if w := want[f.Path]; f.Size != w.Size || f.SHA256 != w.SHA256 {
			t.Errorf("%s の記録が違う: %+v", f.Path, f)
		}
	}
}

func TestExtractZipAcceptsBackslashSeparators(t *testing.T) {
	// 公式の Windows の ZIP は、区切りに \ を使っている
	archivePath := writeZip(t,
		entry{name: `app\dotnet\`},
		entry{name: `app\dotnet\host.dll`, body: "dll"},
		entry{name: `app\meltype-ime.ps1`, body: "script"},
	)
	dest := filepath.Join(t.TempDir(), "out")

	files, err := ExtractZip(archivePath, dest, defaultLimits)
	if err != nil {
		t.Fatal(err)
	}

	if got, err := os.ReadFile(filepath.Join(dest, "app", "meltype-ime.ps1")); err != nil || string(got) != "script" {
		t.Errorf(`\ を区切りとして展開していない: %q, %v`, got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "app", "dotnet", "host.dll")); err != nil || string(got) != "dll" {
		t.Errorf(`末尾が \ の項目をディレクトリとして扱っていない: %q, %v`, got, err)
	}
	if len(files) != 2 || files[1].Path != "app/meltype-ime.ps1" {
		t.Errorf("記録のパスを / 区切りにしていない: %+v", files)
	}
}

func TestExtractZipRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name  string
		entry entry
	}{
		{name: "親への脱出", entry: entry{name: "../evil.txt", body: "x"}},
		{name: "途中の親への脱出", entry: entry{name: "a/../../evil.txt", body: "x"}},
		{name: "絶対パス", entry: entry{name: "/etc/evil", body: "x"}},
		{name: "ドライブ付きのパス", entry: entry{name: "C:/evil.txt", body: "x"}},
		{name: `\ の区切りでの親への脱出`, entry: entry{name: `a\..\..\evil.txt`, body: "x"}},
		{name: `\ で始まる絶対パス`, entry: entry{name: `\evil.txt`, body: "x"}},
		{name: "代替データストリーム", entry: entry{name: "a.txt:hidden", body: "x"}},
		{name: "Windows の予約名", entry: entry{name: "dir/NUL.txt", body: "x"}},
		{name: "Windows の予約名 (小文字)", entry: entry{name: "com1", body: "x"}},
		{name: "末尾のドット", entry: entry{name: "dir./a.txt", body: "x"}},
		{name: "末尾の空白", entry: entry{name: "a.txt ", body: "x"}},
		{name: "シンボリックリンク", entry: entry{name: "link", body: "/etc/passwd", mode: fs.ModeSymlink | 0o777}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archivePath := writeZip(t, tt.entry)
			parent := t.TempDir()
			dest := filepath.Join(parent, "out")

			_, err := ExtractZip(archivePath, dest, defaultLimits)

			if !errors.Is(err, ErrUnsafeArchive) {
				t.Errorf("err = %v, want ErrUnsafeArchive", err)
			}
			entries, _ := os.ReadDir(parent)
			for _, e := range entries {
				if e.Name() != "out" {
					t.Errorf("展開先の外に %s を作った", e.Name())
				}
			}
		})
	}
}

func TestExtractZipRejectsNamesThatCollideIgnoringCase(t *testing.T) {
	archivePath := writeZip(t, entry{name: "Readme.txt", body: "a"}, entry{name: "README.txt", body: "b"})

	_, err := ExtractZip(archivePath, filepath.Join(t.TempDir(), "out"), defaultLimits)

	if !errors.Is(err, ErrUnsafeArchive) {
		t.Errorf("err = %v, want ErrUnsafeArchive", err)
	}
}

func TestExtractZipEnforcesLimits(t *testing.T) {
	t.Run("展開後の合計が上限を超える", func(t *testing.T) {
		archivePath := writeZip(t, entry{name: "big.bin", body: strings.Repeat("0", 2048)})

		_, err := ExtractZip(archivePath, filepath.Join(t.TempDir(), "out"), Limits{MaxFiles: 10, MaxTotalBytes: 1024})

		if !errors.Is(err, ErrUnsafeArchive) {
			t.Errorf("err = %v, want ErrUnsafeArchive", err)
		}
	})
	t.Run("ファイルの数が上限を超える", func(t *testing.T) {
		archivePath := writeZip(t, entry{name: "a", body: "1"}, entry{name: "b", body: "2"}, entry{name: "c", body: "3"})

		_, err := ExtractZip(archivePath, filepath.Join(t.TempDir(), "out"), Limits{MaxFiles: 2, MaxTotalBytes: 1024})

		if !errors.Is(err, ErrUnsafeArchive) {
			t.Errorf("err = %v, want ErrUnsafeArchive", err)
		}
	})
}

func TestExtractZipRequiresEmptyDestination(t *testing.T) {
	archivePath := writeZip(t, entry{name: "a.txt", body: "new"})
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "a.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ExtractZip(archivePath, dest, defaultLimits)

	if err == nil {
		t.Fatal("既存のファイルがある展開先に展開した")
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(got) != "old" {
		t.Errorf("既存のファイルを上書きした: %q", got)
	}
}
