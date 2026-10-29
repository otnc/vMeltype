package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otnc/vmeltype/internal/archive"
	"github.com/otnc/vmeltype/internal/assets"
	"github.com/otnc/vmeltype/internal/catalog"
	"github.com/otnc/vmeltype/internal/catalog/remote"
	"github.com/otnc/vmeltype/internal/prompt"
	"github.com/otnc/vmeltype/internal/store"
	"github.com/otnc/vmeltype/internal/versions"
)

const installLong = `公式の配布物を取得し、GitHub が示す SHA-256 と照合してから展開し、保持版として置きます。有効な版は変えません。

版は v1.4.0 のようなタグか、latest で指定します。latest は、解析できる SemVer が最大の正式版です。--pre を付けると、プレリリースも含めた最大になります。GitHub の Latest の表示とは異なることがあります。
portable 版を取得するには --portable を付けます。付けなければ通常版を取得します。
既に保持している版は、通信せずに終わります。取得し直すには --reinstall を付けます。`

// 公式の ZIP は数十 MB なので、これを大きく超えるものは異常とみなす。
var extractLimits = archive.Limits{MaxFiles: 50000, MaxTotalBytes: 4 << 30}

// サイズが分からない配布物を取得するときの上限。
const fallbackMaxDownloadBytes = 1 << 30

func setupInstallFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("pre", false, "latest でプレリリースも候補にする")
	cmd.Flags().Bool("reinstall", false, "保持済みでも取得し直して検証する")
	cmd.Flags().Bool("allow-unverified", false, "期待値 (SHA-256) のない配布物を、未検証として保持する")
	cmd.Flags().Bool("portable", false, "portable 版を取得する。付けなければ通常版を取得する")
}

type installPlan struct {
	ID             string `json:"id"`
	Tag            string `json:"tag"`
	Prerelease     bool   `json:"prerelease"`
	Asset          string `json:"asset"`
	Size           int64  `json:"size"`
	Destination    string `json:"destination"`
	ExpectedSHA256 string `json:"expectedSha256,omitempty"`
	Reinstall      bool   `json:"reinstall"`
}

func runInstall(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		pre, _ := flags.GetBool("pre")
		reinstall, _ := flags.GetBool("reinstall")
		allowUnverified, _ := flags.GetBool("allow-unverified")
		offline, _ := flags.GetBool("offline")
		dryRun, _ := flags.GetBool("dry-run")
		yes, _ := flags.GetBool("yes")
		asJSON, _ := flags.GetBool("json")
		variant := selectedVariant(cmd)
		if len(args) == 0 {
			return usageError{errors.New("版を指定してください (例: vmeltype install v1.4.0、vmeltype install latest)")}
		}
		input := args[0]

		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		st := store.Store{Root: root}
		out := cmd.OutOrStdout()

		// タグを直接指定したときは、公式一覧を待たずに保持済みかを確かめる
		if input != "latest" && !reinstall {
			if s, ok := findStored(st, input, env.Platform, variant); ok {
				return reportAlreadyStored(out, s, asJSON)
			}
		}

		var c *catalog.Catalog
		if offline {
			c, err = catalog.Load(root)
			if errors.Is(err, catalog.ErrNotFetched) {
				return fmt.Errorf("%s は保持しておらず、保存済みの公式一覧もないので、--offline では取得できません", input)
			}
		} else {
			_, c, err = fetchCatalog(cmd, env, root)
		}
		if err != nil {
			return err
		}
		release, err := resolveRelease(c.Releases(), input, pre)
		if err != nil {
			return err
		}
		asset, err := assets.Select(release, env.Platform, variant)
		if err != nil {
			return err
		}
		id := store.ID(release.Tag, env.Platform.OS, env.Platform.Arch, string(variant))
		if !reinstall {
			if s, err := st.Get(id); err == nil {
				return reportAlreadyStored(out, s, asJSON)
			}
		}
		if offline {
			return fmt.Errorf("%s は保持していないので、--offline では取得できません", release.Tag)
		}

		expected := ""
		if hex, ok := strings.CutPrefix(asset.Digest, "sha256:"); ok {
			expected = strings.ToLower(hex)
		}
		if expected == "" && !allowUnverified {
			return fmt.Errorf("%s には公式の期待値 (SHA-256) がないので保持しません。未検証として保持するなら --allow-unverified を指定してください", asset.Name)
		}
		if asset.Name != filepath.Base(asset.Name) || strings.ContainsAny(asset.Name, `:\/`) {
			return fmt.Errorf("配布物の名前 %q はファイル名に使えません", asset.Name)
		}

		plan := installPlan{
			ID:             id,
			Tag:            release.Tag,
			Prerelease:     release.Prerelease,
			Asset:          asset.Name,
			Size:           asset.Size,
			Destination:    st.Path(id),
			ExpectedSHA256: expected,
			Reinstall:      reinstall,
		}
		planOut := out
		if asJSON {
			planOut = cmd.ErrOrStderr()
		}
		writeInstallPlan(planOut, plan)
		if dryRun {
			if asJSON {
				return writeJSON(out, map[string]any{"dryRun": true, "plan": plan})
			}
			return nil
		}

		confirmer := prompt.Confirmer{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr(), Interactive: env.Interactive && !asJSON, AssumeYes: yes}
		if err := confirmer.Confirm(fmt.Sprintf("%s を取得しますか？", release.Tag), prompt.DefaultYes); err != nil {
			return err
		}

		stored, err := downloadAndStore(cmd, env, st, plan, release, asset)
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(out, map[string]any{"alreadyStored": false, "stored": stored})
		}
		fmt.Fprintf(out, "%s を保持しました (保持 ID: %s)。有効にするには \"vmeltype use %s\" を実行します。\n", stored.Tag, stored.ID, stored.Tag)
		return nil
	}
}

// downloadAndStore は、作業用のディレクトリで取得、照合、展開を終えてから保持版として置く。
// 途中で失敗したら、作業用のディレクトリごと捨てる。
func downloadAndStore(cmd *cobra.Command, env *Env, st store.Store, plan installPlan, release catalog.Release, asset catalog.Asset) (store.Stored, error) {
	staging, err := st.Stage()
	if err != nil {
		return store.Stored{}, err
	}
	committed := false
	defer func() {
		if !committed {
			staging.Discard()
		}
	}()

	archivePath := filepath.Join(staging.Dir(), asset.Name)
	file, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return store.Stored{}, err
	}
	maxBytes := asset.Size
	if maxBytes <= 0 {
		maxBytes = fallbackMaxDownloadBytes
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s を取得しています...\n", asset.Name)
	hash := sha256.New()
	_, err = newRemoteClient(env).Download(cmd.Context(), asset.DownloadURL, maxBytes, io.MultiWriter(file, hash))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return store.Stored{}, err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if plan.ExpectedSHA256 != "" && actual != plan.ExpectedSHA256 {
		return store.Stored{}, fmt.Errorf("%s の SHA-256 が公式の期待値と一致しません (期待値 %s、取得したもの %s)。保持しません", asset.Name, plan.ExpectedSHA256, actual)
	}

	files, err := archive.ExtractZip(archivePath, filepath.Join(staging.Dir(), "files"), extractLimits)
	if err != nil {
		return store.Stored{}, err
	}
	record := store.ArchiveRecord{File: asset.Name, SHA256: actual}
	if plan.ExpectedSHA256 != "" {
		record.Expected = plan.ExpectedSHA256
		record.ExpectedSource = store.ExpectedFromGitHubDigest
	}
	stored := store.Stored{
		ID:         plan.ID,
		Tag:        release.Tag,
		Prerelease: release.Prerelease,
		OS:         env.Platform.OS,
		Arch:       env.Platform.Arch,
		Variant:    plan.ID[strings.LastIndex(plan.ID, "_")+1:],
		Asset:      store.Asset{ID: asset.ID, Name: asset.Name, Size: asset.Size, DownloadURL: asset.DownloadURL},
		Archive:    record,
		Files:      files,
		StoredAt:   env.Now().UTC(),
	}
	if err := staging.Commit(stored); err != nil {
		return store.Stored{}, err
	}
	committed = true
	return stored, nil
}

func resolveRelease(releases []catalog.Release, input string, pre bool) (catalog.Release, error) {
	if input == "latest" {
		release, ok := versions.Latest(releases, pre)
		if !ok {
			return catalog.Release{}, errors.New("公式一覧に latest の候補がありません")
		}
		return release, nil
	}
	return versions.ResolveTag(releases, input)
}

// findStored は、タグの指定 (先頭の v の有無を含む) に合う保持版を、候補が 1 つのときだけ返す。
func findStored(st store.Store, input string, platform assets.Platform, variant assets.Variant) (store.Stored, bool) {
	alternative := "v" + input
	if after, ok := strings.CutPrefix(input, "v"); ok {
		alternative = after
	}
	if s, err := st.Get(store.ID(input, platform.OS, platform.Arch, string(variant))); err == nil {
		return s, true
	}
	if s, err := st.Get(store.ID(alternative, platform.OS, platform.Arch, string(variant))); err == nil {
		return s, true
	}
	return store.Stored{}, false
}

func reportAlreadyStored(w io.Writer, s store.Stored, asJSON bool) error {
	if asJSON {
		return writeJSON(w, map[string]any{"alreadyStored": true, "stored": s})
	}
	fmt.Fprintf(w, "%s は既に保持しています (保持 ID: %s)。取得し直すには --reinstall を付けます。\n", s.Tag, s.ID)
	return nil
}

func writeInstallPlan(w io.Writer, plan installPlan) {
	kind := "正式版"
	if plan.Prerelease {
		kind = "プレリリース"
	}
	expected := "公式の SHA-256 と照合する"
	if plan.ExpectedSHA256 == "" {
		expected = "期待値がないので照合しない (未検証として保持する)"
	}
	fmt.Fprintf(w, "取得する版:   %s (%s)\n", plan.Tag, kind)
	fmt.Fprintf(w, "配布物:       %s (%s)\n", plan.Asset, formatBytes(plan.Size))
	fmt.Fprintf(w, "置き場所:     %s\n", plan.Destination)
	fmt.Fprintf(w, "検証:         %s\n", expected)
	if plan.Reinstall {
		fmt.Fprintln(w, "既に保持していれば、検証が済んでから置き換えます。")
	}
	fmt.Fprintln(w, "有効な版は変えません。")
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func newRemoteClient(env *Env) *remote.Client {
	return &remote.Client{
		HTTP:      env.HTTPClient,
		BaseURL:   env.GitHubAPIURL,
		Token:     env.Getenv("VMELTYPE_GITHUB_TOKEN"),
		UserAgent: "vmeltype/" + env.Version,
		Now:       env.Now,
	}
}

// selectedVariant は、--portable の有無から対象の variant を返す。portable 版は明示したときだけ扱い、付けなければ通常版にする。
func selectedVariant(cmd *cobra.Command) assets.Variant {
	if portable, _ := cmd.Flags().GetBool("portable"); portable {
		return assets.Portable
	}
	return assets.Standard
}
