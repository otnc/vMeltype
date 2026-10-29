package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleCatalog() *Catalog {
	return &Catalog{
		Repository:    "yksr-melt/Meltype",
		FetchedAt:     time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC),
		RevalidatedAt: time.Date(2026, 10, 29, 9, 30, 0, 0, time.UTC),
		Complete:      true,
		Pages: []Page{
			{URL: "https://api.example.com/page1", ETag: `"a"`, Releases: []Release{
				{ID: 2, Tag: "v1.4.0", PublishedAt: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)},
				{ID: 1, Tag: "v1.3.1-pre.3", Prerelease: true, PublishedAt: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)},
			}},
			{URL: "https://api.example.com/page2", ETag: `"b"`, Releases: []Release{
				{ID: 3, Tag: "v1.3.0", PublishedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)},
				// 取得の途中で新しいリリースが出ると、前のページの最後がこのページの先頭にずれて重なる
				{ID: 1, Tag: "v1.3.1-pre.3", Prerelease: true, PublishedAt: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)},
			}},
		},
	}
}

func TestSaveAndLoad(t *testing.T) {
	root := t.TempDir()

	if err := Save(root, sampleCatalog()); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	if got.Repository != "yksr-melt/Meltype" || !got.FetchedAt.Equal(sampleCatalog().FetchedAt) || len(got.Pages) != 2 || got.Pages[1].ETag != `"b"` {
		t.Errorf("保存した内容と読んだ内容が異なる: %+v", got)
	}
}

func TestSaveReplacesPreviousCatalog(t *testing.T) {
	root := t.TempDir()
	first := sampleCatalog()
	second := sampleCatalog()
	second.Pages = second.Pages[:1]

	if err := Save(root, first); err != nil {
		t.Fatal(err)
	}
	if err := Save(root, second); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Pages) != 1 {
		t.Errorf("前の一覧が置き換わっていない: %d ページ", len(got.Pages))
	}
	entries, _ := os.ReadDir(filepath.Join(root, "catalog"))
	if len(entries) != 1 {
		t.Errorf("一時ファイルが残っている: %v", entries)
	}
}

func TestLoadWithoutCatalog(t *testing.T) {
	_, err := Load(t.TempDir())

	if !errors.Is(err, ErrNotFetched) {
		t.Errorf("err = %v, want ErrNotFetched", err)
	}
}

func TestLoadRejectsNewerSchema(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "catalog")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "releases.json"), []byte(`{"schemaVersion": 999}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(root)

	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Errorf("err = %v, want ErrUnsupportedSchema", err)
	}
}

func TestReleasesRemovesDuplicatesAndSortsByPublishedDate(t *testing.T) {
	got := sampleCatalog().Releases()

	var tags []string
	for _, r := range got {
		tags = append(tags, r.Tag)
	}
	want := []string{"v1.4.0", "v1.3.1-pre.3", "v1.3.0"}
	if len(tags) != len(want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Errorf("tags = %v, want %v", tags, want)
			break
		}
	}
}
