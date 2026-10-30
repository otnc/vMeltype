package privatefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMkdirAllCreatesOwnerOnlyDirectories(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "Meltool", "versions")

	if err := MkdirAll(dir); err != nil {
		t.Fatal(err)
	}

	for _, created := range []string{filepath.Join(parent, "Meltool"), dir} {
		if others := othersCanAccess(t, created); others != "" {
			t.Errorf("%s を本人のほかも使える: %s", created, others)
		}
	}
}

func TestFilesInsideInheritOwnerOnlyAccess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "root")
	if err := MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "manifest.json")

	if err := WriteFile(file, []byte("{}")); err != nil {
		t.Fatal(err)
	}

	if others := othersCanAccess(t, file); others != "" {
		t.Errorf("作ったファイルを本人のほかも使える: %s", others)
	}
}

func TestMkdirAllKeepsExistingDirectories(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "keep")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := MkdirAll(dir); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(marker); err != nil {
		t.Errorf("既存のディレクトリの中身が消えた: %v", err)
	}
}
