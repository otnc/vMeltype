// Package archive は、公式の ZIP を安全に展開し、展開したファイルの検査値を記録する。
package archive

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/otnc/vmeltype/internal/privatefs"
)

// ErrUnsafeArchive は、展開先の外へ書く、OS の特殊な名前を使う、大きすぎるなど、安全に展開できない ZIP を表す。
var ErrUnsafeArchive = errors.New("安全に展開できない ZIP です")

// Limits は、展開の上限。ZIP のヘッダーのサイズは信用せず、実際に書いた量で数える。
type Limits struct {
	MaxFiles      int
	MaxTotalBytes int64
}

// File は、展開したファイル 1 つの記録。Path は展開先からの / 区切りの相対パス。
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

var windowsReservedName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9¹²³]|lpt[0-9¹²³])(\..*)?$`)

// ExtractZip は、zipPath を空の dest に展開し、展開したファイルの記録を返す。
// 安全でない項目が 1 つでもあれば、何かを書く前に止める。
func ExtractZip(zipPath, dest string, limits Limits) ([]File, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("ZIP を開けませんでした: %w", err)
	}
	defer r.Close()

	if err := validateEntries(r.File, limits); err != nil {
		return nil, err
	}
	if err := requireEmpty(dest); err != nil {
		return nil, err
	}

	var files []File
	var written int64
	for _, f := range r.File {
		name := entryName(f)
		target := filepath.Join(dest, filepath.FromSlash(name))
		if isDirEntry(f) {
			if err := privatefs.MkdirAll(target); err != nil {
				return nil, err
			}
			continue
		}
		if err := privatefs.MkdirAll(filepath.Dir(target)); err != nil {
			return nil, err
		}
		file, n, err := extractFile(f, target, limits.MaxTotalBytes-written)
		if err != nil {
			return nil, err
		}
		written += n
		file.Path = name
		files = append(files, file)
	}
	return files, nil
}

func validateEntries(entries []*zip.File, limits Limits) error {
	if len(entries) > limits.MaxFiles {
		return fmt.Errorf("%w: 項目が %d 個あり、上限の %d 個を超えています", ErrUnsafeArchive, len(entries), limits.MaxFiles)
	}
	seen := map[string]bool{}
	for _, f := range entries {
		if err := validateName(entryName(f)); err != nil {
			return err
		}
		mode := f.Mode()
		if !mode.IsRegular() && !mode.IsDir() {
			return fmt.Errorf("%w: %q は通常のファイルでもディレクトリでもありません", ErrUnsafeArchive, f.Name)
		}
		// Windows と macOS の既定のファイルシステムは大文字と小文字を区別しないので、同じ場所への上書きになる
		key := strings.ToLower(entryName(f))
		if seen[key] {
			return fmt.Errorf("%w: 大文字と小文字だけが違う名前が重なっています: %q", ErrUnsafeArchive, f.Name)
		}
		seen[key] = true
	}
	return nil
}

// entryName は、項目の名前を / 区切りにそろえて返す。公式の Windows の ZIP は \ を区切りに使うため、
// \ も区切りとみなしてから検査し、\ を使った親への脱出も見逃さないようにする。
func entryName(f *zip.File) string {
	return path.Clean(strings.ReplaceAll(f.Name, `\`, "/"))
}

func validateName(name string) error {
	unsafe := func(reason string) error {
		return fmt.Errorf("%w: %q は%s", ErrUnsafeArchive, name, reason)
	}
	switch {
	case name == "" || name == ".":
		return unsafe("空の名前です")
	case strings.HasPrefix(name, "/"):
		return unsafe("絶対パスです")
	case strings.ContainsRune(name, 0):
		return unsafe("使えない文字を含みます")
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return unsafe("展開先の外を指します")
	}
	for _, element := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		switch {
		case element == "..":
			return unsafe("親のディレクトリを指します")
		case strings.Contains(element, ":"):
			return unsafe("ドライブか代替データストリームを指します")
		case windowsReservedName.MatchString(element):
			return unsafe("Windows の予約名を含みます")
		case element != "." && (strings.HasSuffix(element, ".") || strings.HasSuffix(element, " ")):
			return unsafe("Windows で末尾が消える名前を含みます")
		}
	}
	return nil
}

func requireEmpty(dest string) error {
	if err := privatefs.MkdirAll(dest); err != nil {
		return err
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("展開先 %s が空ではありません", dest)
	}
	return nil
}

func extractFile(f *zip.File, target string, remaining int64) (File, int64, error) {
	src, err := f.Open()
	if err != nil {
		return File{}, 0, fmt.Errorf("%q を読めませんでした: %w", f.Name, err)
	}
	defer src.Close()

	perm := fs.FileMode(0o600)
	if f.Mode().Perm()&0o111 != 0 {
		perm = 0o700
	}
	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return File{}, 0, err
	}
	hash := sha256.New()
	// 上限より 1 バイト多く読み、超えたかを確かめる
	n, err := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(src, remaining+1))
	closeErr := dst.Close()
	if err != nil {
		return File{}, 0, fmt.Errorf("%q を展開できませんでした: %w", f.Name, err)
	}
	if closeErr != nil {
		return File{}, 0, closeErr
	}
	if n > remaining {
		return File{}, 0, fmt.Errorf("%w: 展開した量が上限を超えました", ErrUnsafeArchive)
	}
	return File{Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}, n, nil
}

// isDirEntry は、項目がディレクトリかを返す。公式の Windows の ZIP は、ディレクトリの名前の末尾も \ にしている。
func isDirEntry(f *zip.File) bool {
	return f.FileInfo().IsDir() || strings.HasSuffix(f.Name, `\`)
}
