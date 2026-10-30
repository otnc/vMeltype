package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/otnc/meltool/internal/catalog"
)

const releasesLong = `本家の GitHub Releases から公式のリリースの一覧を取得し、保存してから表示します。
取得に失敗したときは、古い一覧を最新のように表示せずに終了します。保存済みの一覧は --cached で表示できます。

既定では正式版だけを表示し、--all でプレリリースも含めます。
環境変数 MELTOOL_GITHUB_TOKEN があれば、GitHub の API の認証に使います。`

func setupReleasesFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("cached", false, "取得せず、保存済みの一覧を表示する")
}

func runFetch(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		previous, fetched, err := fetchCatalog(cmd, env, root)
		if err != nil {
			return err
		}
		changed := previous == nil || !previous.FetchedAt.Equal(fetched.FetchedAt)
		stable, pre := countReleases(fetched.Releases())

		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"repository":    fetched.Repository,
				"fetchedAt":     fetched.FetchedAt,
				"revalidatedAt": fetched.RevalidatedAt,
				"changed":       changed,
				"stable":        stable,
				"prerelease":    pre,
			})
		}
		state := "公式一覧を取得しました"
		if !changed {
			state = "公式一覧は前回の取得から変わっていません"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s (正式版 %d 件、プレリリース %d 件)\n", state, stable, pre)
		return nil
	}
}

func runReleases(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		cached, _ := cmd.Flags().GetBool("cached")
		offline, _ := cmd.Flags().GetBool("offline")
		all, _ := cmd.Flags().GetBool("all")
		asJSON, _ := cmd.Flags().GetBool("json")
		if offline && !cached {
			return usageError{errors.New("--offline では公式一覧を取得できません。保存済みの一覧は \"meltool releases --cached\" で表示します")}
		}

		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		var c *catalog.Catalog
		source := "remote"
		if cached {
			source = "cache"
			c, err = catalog.Load(root)
			if errors.Is(err, catalog.ErrNotFetched) {
				return errors.New("保存済みの公式一覧がありません。\"meltool fetch\" で取得してください")
			}
		} else {
			_, c, err = fetchCatalog(cmd, env, root)
		}
		if err != nil {
			return err
		}

		releases := c.Releases()
		var shown []catalog.Release
		for _, r := range releases {
			if all || !r.Prerelease {
				shown = append(shown, r)
			}
		}
		if asJSON {
			return writeJSON(cmd.OutOrStdout(), releasesJSON{
				Repository:    c.Repository,
				Source:        source,
				FetchedAt:     c.FetchedAt,
				RevalidatedAt: c.RevalidatedAt,
				Complete:      c.Complete,
				Releases:      nonNil(shown),
			})
		}
		writeReleases(cmd.OutOrStdout(), c, shown, len(releases)-len(shown), cached, env.Now())
		return nil
	}
}

type releasesJSON struct {
	Repository    string            `json:"repository"`
	Source        string            `json:"source"`
	FetchedAt     time.Time         `json:"fetchedAt"`
	RevalidatedAt time.Time         `json:"revalidatedAt"`
	Complete      bool              `json:"complete"`
	Releases      []catalog.Release `json:"releases"`
}

// fetchCatalog は、公式一覧を取得して保存し、取得の前に保存されていた一覧と、新しい一覧を返す。
// 保存済みの一覧は、失敗したときに置き換えない。
func fetchCatalog(cmd *cobra.Command, env *Env, root string) (previous, fetched *catalog.Catalog, err error) {
	previous, err = catalog.Load(root)
	hasSaved := err == nil
	switch {
	case errors.Is(err, catalog.ErrUnsupportedSchema):
		return nil, nil, err
	case err != nil:
		// 一覧は取得し直せるので、読めない保存済みの一覧は捨てて取り直す
		previous = nil
	}

	fetched, err = newRemoteClient(env).Fetch(cmd.Context(), catalog.Repository, previous)
	if err != nil {
		if hasSaved {
			return nil, nil, fmt.Errorf("公式一覧を取得できませんでした: %w\n保存済みの一覧は \"meltool releases --cached\" で表示できます", err)
		}
		return nil, nil, fmt.Errorf("公式一覧を取得できませんでした: %w", err)
	}
	if err := catalog.Save(root, fetched); err != nil {
		return nil, nil, err
	}
	return previous, fetched, nil
}

func writeReleases(w io.Writer, c *catalog.Catalog, shown []catalog.Release, hidden int, cached bool, now time.Time) {
	if cached {
		fmt.Fprintf(w, "保存済みの公式一覧 (%s)。最新とは限りません\n", c.Repository)
		fmt.Fprintf(w, "最後に確かめた日時: %s (%s)\n\n", formatLocal(c.RevalidatedAt), formatAge(now.Sub(c.RevalidatedAt)))
	} else {
		fmt.Fprintf(w, "公式のリリース (%s、%s に取得)\n\n", c.Repository, formatLocal(c.RevalidatedAt))
	}

	if len(shown) == 0 {
		fmt.Fprintln(w, "表示するリリースがありません。")
	} else {
		rows := [][]string{{"タグ", "区分", "公開日", "配布物", "URL"}}
		for _, r := range shown {
			kind := "正式版"
			if r.Prerelease {
				kind = "プレリリース"
			}
			assets := fmt.Sprintf("%d 件", len(r.Assets))
			if len(r.Assets) == 0 {
				assets = "なし"
			}
			rows = append(rows, []string{r.Tag, kind, r.PublishedAt.Local().Format("2006-01-02"), assets, r.URL})
		}
		writeTable(w, rows)
	}
	if hidden > 0 {
		fmt.Fprintf(w, "\nプレリリースを %d 件省きました。--all で表示します。\n", hidden)
	}
}

func writeTable(w io.Writer, rows [][]string) {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], displayWidth(cell))
		}
	}
	for _, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			line.WriteString(cell)
			if i < len(row)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-displayWidth(cell)+2))
			}
		}
		fmt.Fprintln(w, strings.TrimRight(line.String(), " "))
	}
}

// displayWidth は、端末で日本語が半角 2 文字分の幅を取ることを考えた、表示の幅を返す。
func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) || (r >= '！' && r <= '｠') || (r >= '　' && r <= '〿') {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func countReleases(releases []catalog.Release) (stable, pre int) {
	for _, r := range releases {
		if r.Prerelease {
			pre++
		} else {
			stable++
		}
	}
	return stable, pre
}

func formatLocal(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04")
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "1 分以内"
	case d < time.Hour:
		return fmt.Sprintf("%d 分前", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d 時間前", int(d.Hours()))
	default:
		return fmt.Sprintf("%d 日前", int(d.Hours()/24))
	}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
