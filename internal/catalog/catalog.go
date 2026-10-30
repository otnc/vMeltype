// Package catalog は、本家の公式一覧を表す型と、その保存と読み込みを扱う。
// 通信は remote パッケージに分け、このパッケージはローカルのファイルだけを読み書きする。
package catalog

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/otnc/vmeltype/internal/privatefs"
)

// Repository は、管理する本家のリポジトリ。
const Repository = "yksr-melt/Meltype"

// SchemaVersion は、このバージョンの vMeltype が読み書きする保存形式のバージョン。
const SchemaVersion = 1

var (
	// ErrNotFetched は、公式一覧をまだ一度も保存していないことを表す。
	ErrNotFetched = errors.New("公式一覧をまだ取得していません")
	// ErrUnsupportedSchema は、新しいバージョンの vMeltype が保存した形式で、読めないことを表す。
	ErrUnsupportedSchema = errors.New("保存済みの公式一覧は新しい形式なので読めません。vMeltype を更新してください")
)

// Catalog は、全ページの取得を終えた公式一覧。
type Catalog struct {
	SchemaVersion int    `json:"schemaVersion"`
	Repository    string `json:"repository"`
	// FetchedAt は、内容が最後に変わった取得の時刻。
	FetchedAt time.Time `json:"fetchedAt"`
	// RevalidatedAt は、全ページが最新であることを最後に確かめた時刻。
	RevalidatedAt time.Time `json:"revalidatedAt"`
	Complete      bool      `json:"complete"`
	// Pages は、API のページごとの内容。次の取得で ETag を使って変わっていないページを省くために残す。
	Pages []Page `json:"pages"`
}

// Page は、Releases API の 1 ページ分。
type Page struct {
	URL      string    `json:"url"`
	ETag     string    `json:"etag,omitempty"`
	Releases []Release `json:"releases"`
}

// Release は、公開されたリリース。Draft は含めない。
type Release struct {
	ID          int64     `json:"id"`
	Tag         string    `json:"tag"`
	Name        string    `json:"name"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"publishedAt"`
	URL         string    `json:"url"`
	Assets      []Asset   `json:"assets"`
}

// Asset は、リリースに添付された配布物。URL はタグから組み立てず、API の返した値を使う。
type Asset struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
	// Digest は、GitHub が計算した "sha256:..." の形の値。古い配布物では空のことがある。
	Digest      string `json:"digest,omitempty"`
	DownloadURL string `json:"downloadUrl"`
}

// Releases は、全ページのリリースを、重複を除いて公開日の新しい順に返す。
// 取得の途中で新しいリリースが公開されると、ページの境目で同じリリースが重なるため、ID で除く。
func (c *Catalog) Releases() []Release {
	seen := map[int64]bool{}
	var releases []Release
	for _, page := range c.Pages {
		for _, r := range page.Releases {
			if seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			releases = append(releases, r)
		}
	}
	slices.SortStableFunc(releases, func(a, b Release) int {
		return cmp.Compare(b.PublishedAt.UnixNano(), a.PublishedAt.UnixNano())
	})
	return releases
}

func catalogPath(root string) string {
	return filepath.Join(root, "catalog", "releases.json")
}

// Load は、保存済みの公式一覧を読む。
func Load(root string) (*Catalog, error) {
	data, err := os.ReadFile(catalogPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFetched
	}
	if err != nil {
		return nil, fmt.Errorf("保存済みの公式一覧を読めませんでした: %w", err)
	}
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("保存済みの公式一覧が壊れています: %w", err)
	}
	if c.SchemaVersion > SchemaVersion {
		return nil, ErrUnsupportedSchema
	}
	return &c, nil
}

// Save は、公式一覧を保存する。同じディレクトリの一時ファイルへ書いてから置き換えるので、
// 途中で失敗しても、前の完全な一覧が残る。
func Save(root string, c *Catalog) error {
	dir := filepath.Dir(catalogPath(root))
	if err := privatefs.MkdirAll(dir); err != nil {
		return fmt.Errorf("公式一覧の保存先を作れませんでした: %w", err)
	}
	saved := *c
	saved.SchemaVersion = SchemaVersion
	data, err := json.MarshalIndent(&saved, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "releases-*.json.tmp")
	if err != nil {
		return fmt.Errorf("公式一覧を保存できませんでした: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("公式一覧を保存できませんでした: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("公式一覧を保存できませんでした: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("公式一覧を保存できませんでした: %w", err)
	}
	if err := os.Rename(tmp.Name(), catalogPath(root)); err != nil {
		return fmt.Errorf("公式一覧を保存できませんでした: %w", err)
	}
	return nil
}
