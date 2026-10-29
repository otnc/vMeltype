package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/otnc/vmeltype/internal/archive"
)

func sample(tag string) Stored {
	return Stored{
		ID:       ID(tag, "windows", "amd64", "standard"),
		Tag:      tag,
		OS:       "windows",
		Arch:     "amd64",
		Variant:  "standard",
		Asset:    Asset{ID: 30, Name: "Meltype-1.4.0-windows.zip", Size: 10, DownloadURL: "https://example.com/a.zip"},
		Archive:  ArchiveRecord{File: "Meltype-1.4.0-windows.zip", SHA256: "abc", Expected: "abc", ExpectedSource: ExpectedFromGitHubDigest},
		Files:    []archive.File{{Path: "Meltype/Meltype.exe", Size: 6, SHA256: "def"}},
		StoredAt: time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC),
	}
}

func commit(t *testing.T, s Store, stored Stored, payload string) {
	t.Helper()
	staging, err := s.Stage()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging.Dir(), "payload"), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := staging.Commit(stored); err != nil {
		t.Fatal(err)
	}
}

func TestCommittedVersionsAreListed(t *testing.T) {
	s := Store{Root: t.TempDir()}
	commit(t, s, sample("v1.4.0"), "a")
	commit(t, s, sample("v1.3.0"), "b")

	got, problems, err := s.List()

	if err != nil || len(problems) != 0 {
		t.Fatalf("err = %v, problems = %v", err, problems)
	}
	if len(got) != 2 || got[0].Tag != "v1.4.0" || got[1].Tag != "v1.3.0" {
		t.Errorf("SemVer の新しい順に並んでいない: %+v", got)
	}
	if got[0].Files[0].SHA256 != "def" {
		t.Errorf("manifest の内容が残っていない: %+v", got[0])
	}
}

func TestUnfinishedStagingIsNotListed(t *testing.T) {
	s := Store{Root: t.TempDir()}
	staging, err := s.Stage()
	if err != nil {
		t.Fatal(err)
	}

	got, problems, err := s.List()

	if err != nil || len(got) != 0 || len(problems) != 0 {
		t.Errorf("作業中の保持版を一覧に出した: %+v, %v, %v", got, problems, err)
	}
	if err := staging.Discard(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staging.Dir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("作業用のディレクトリが残っている")
	}
}

func TestCommitReplacesExistingVersion(t *testing.T) {
	s := Store{Root: t.TempDir()}
	commit(t, s, sample("v1.4.0"), "old")
	commit(t, s, sample("v1.4.0"), "new")

	got, err := s.Get(ID("v1.4.0", "windows", "amd64", "standard"))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := os.ReadFile(filepath.Join(s.Path(got.ID), "payload"))
	if string(payload) != "new" {
		t.Errorf("置き換わっていない: %q", payload)
	}
	entries, _ := os.ReadDir(filepath.Join(s.Root, "versions"))
	if len(entries) != 1 {
		t.Errorf("置き換え前の保持版が残っている: %v", entries)
	}
}

func TestBrokenManifestIsReportedAsProblem(t *testing.T) {
	s := Store{Root: t.TempDir()}
	commit(t, s, sample("v1.4.0"), "a")
	broken := filepath.Join(s.Root, "versions", "v1.3.0_windows-amd64_standard")
	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}

	got, problems, err := s.List()

	if err != nil || len(got) != 1 {
		t.Fatalf("壊れた保持版のせいで正常なものまで読めない: %+v, %v", got, err)
	}
	if len(problems) != 1 || problems[0].ID != "v1.3.0_windows-amd64_standard" {
		t.Errorf("壊れた保持版を報告していない: %+v", problems)
	}
}

func TestGetUnknownVersion(t *testing.T) {
	_, err := Store{Root: t.TempDir()}.Get("v9.9.9_windows-amd64_standard")

	if !errors.Is(err, ErrNotStored) {
		t.Errorf("err = %v, want ErrNotStored", err)
	}
}

func TestIDIsSafeAsDirectoryName(t *testing.T) {
	got := ID("v1.3.1-pre.3/../x", "windows", "amd64", "standard")

	if filepath.Base(got) != got || got == "" {
		t.Errorf("ディレクトリ名に使えない ID: %q", got)
	}
}
