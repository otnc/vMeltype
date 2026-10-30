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

	"github.com/otnc/vmeltype/internal/catalog"
	"github.com/otnc/vmeltype/internal/prompt"
	"github.com/otnc/vmeltype/internal/selfupdate"
)

const appUpdateLong = `vMeltype の新しいバージョンを GitHub のリリース (otnc/vMeltype) から取得し、いま動いている vmeltype.exe を置き換えます。Meltype のバージョンは変えません。

既定は正式版の最新です。--pre を付けたときだけ、プレリリースも含めた最新を選びます。いまより古いバージョンには下げません。
配布物は GitHub が示す SHA-256 と照合してから置き換えます。前の vmeltype.exe は vmeltype.exe.old に退避し、次の更新で消します。`

func setupAppUpdateFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("pre", false, "プレリリースも含めて探す")
}

// suggestPre は、プレリリースを使っているのに --pre なしで探して最新だったときに、--pre を案内する。
// 正式版だけを探すと、いまのプレリリースより新しい正式版がまだなければ、プレリリースの新しいものを見落とすため。
func suggestPre(out io.Writer, version string, pre bool) {
	if !pre && strings.Contains(version, "-") {
		fmt.Fprintln(out, "プレリリースも含めて探すなら --pre を付けます。")
	}
}

func isDevBuild(version string) bool {
	return version == "dev" || version == ""
}

// findSelfUpdate は、vMeltype 自身のリリースから、いまより新しく、この端末の配布物があるものの最大を探す。
func findSelfUpdate(cmd *cobra.Command, env *Env, pre bool) (catalog.Release, catalog.Asset, bool, error) {
	c, err := newRemoteClient(env).Fetch(cmd.Context(), selfupdate.Repository, nil)
	if err != nil {
		return catalog.Release{}, catalog.Asset{}, false, fmt.Errorf("vMeltype のリリースの一覧を取得できませんでした: %w", err)
	}
	release, asset, ok := selfupdate.Select(c.Releases(), env.Version, pre, env.Platform.OS, env.Platform.Arch)
	return release, asset, ok, nil
}

func runAppCheck(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		pre, _ := cmd.Flags().GetBool("pre")
		asJSON, _ := cmd.Flags().GetBool("json")
		if isDevBuild(env.Version) {
			return errors.New("開発中のビルドなので、リリースと比べられません。リリースの vmeltype.exe を使ってください")
		}
		release, _, ok, err := findSelfUpdate(cmd, env, pre)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		latest := env.Version
		if ok {
			latest = release.Tag
		}
		switch {
		case asJSON:
			if err := writeJSON(out, map[string]any{"current": env.Version, "latest": latest, "updateAvailable": ok}); err != nil {
				return err
			}
		case ok:
			command := "vmeltype app update"
			if release.Prerelease {
				command += " --pre"
			}
			fmt.Fprintf(out, "vMeltype の新しいバージョンがあります: %s (いまは %s)。\"%s\" で更新します。\n", release.Tag, env.Version, command)
		default:
			fmt.Fprintf(out, "vMeltype %s は最新です。\n", env.Version)
			suggestPre(out, env.Version, pre)
		}
		if ok {
			return errUpdateAvailable
		}
		return nil
	}
}

func runAppUpdate(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		flags := cmd.Flags()
		pre, _ := flags.GetBool("pre")
		dryRun, _ := flags.GetBool("dry-run")
		yes, _ := flags.GetBool("yes")
		asJSON, _ := flags.GetBool("json")
		if isDevBuild(env.Version) {
			return errors.New("開発中のビルドなので、更新しません。リリースの vmeltype.exe を使ってください")
		}
		exe, err := env.Executable()
		if err != nil {
			return fmt.Errorf("vmeltype.exe の場所が分かりませんでした: %w", err)
		}

		release, asset, ok, err := findSelfUpdate(cmd, env, pre)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if !ok {
			if asJSON {
				return writeJSON(out, map[string]any{"changed": false, "current": env.Version})
			}
			fmt.Fprintf(out, "vMeltype %s は最新です。\n", env.Version)
			suggestPre(out, env.Version, pre)
			return nil
		}
		expected, ok := strings.CutPrefix(asset.Digest, "sha256:")
		if !ok || expected == "" {
			return fmt.Errorf("%s には GitHub の期待値 (SHA-256) がないので、置き換えません", asset.Name)
		}
		expected = strings.ToLower(expected)

		planOut := out
		if asJSON {
			planOut = cmd.ErrOrStderr()
		}
		fmt.Fprintf(planOut, "いまのバージョン: %s\n", env.Version)
		fmt.Fprintf(planOut, "新しいバージョン: %s\n", release.Tag)
		fmt.Fprintf(planOut, "配布物:           %s (%s)\n", asset.Name, formatBytes(asset.Size))
		fmt.Fprintf(planOut, "置き換え先:       %s\n", exe)
		fmt.Fprintln(planOut, "GitHub が示す SHA-256 と照合してから置き換えます。Meltype のバージョンは変えません。")
		if dryRun {
			if asJSON {
				return writeJSON(out, map[string]any{"dryRun": true, "current": env.Version, "latest": release.Tag, "executable": exe})
			}
			return nil
		}
		confirmer := env.confirmer(cmd, env.Interactive && !asJSON, yes)
		if err := confirmer.Confirm(fmt.Sprintf("vMeltype を %s に更新しますか？", release.Tag), prompt.DefaultYes); err != nil {
			return err
		}

		// 置き換えは rename で行うので、取得したものも同じディレクトリ (同じドライブ) に置く
		archive, err := os.CreateTemp(filepath.Dir(exe), "vmeltype-update-*.zip")
		if err != nil {
			return err
		}
		defer os.Remove(archive.Name())
		fmt.Fprintf(cmd.ErrOrStderr(), "%s を取得しています...\n", asset.Name)
		hash := sha256.New()
		maxBytes := asset.Size
		if maxBytes <= 0 {
			maxBytes = fallbackMaxDownloadBytes
		}
		_, err = newRemoteClient(env).Download(cmd.Context(), asset.DownloadURL, maxBytes, io.MultiWriter(archive, hash))
		if closeErr := archive.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if actual := hex.EncodeToString(hash.Sum(nil)); actual != expected {
			return fmt.Errorf("%s の SHA-256 が GitHub の期待値と一致しません (期待値 %s、取得したもの %s)。置き換えません", asset.Name, expected, actual)
		}
		if err := selfupdate.Apply(exe, archive.Name()); err != nil {
			return err
		}
		if asJSON {
			return writeJSON(out, map[string]any{"changed": true, "previous": env.Version, "current": release.Tag})
		}
		fmt.Fprintf(out, "vMeltype を %s に更新しました。\n", release.Tag)
		return nil
	}
}
