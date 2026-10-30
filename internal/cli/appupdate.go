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

	"github.com/otnc/meltool/internal/prompt"
	"github.com/otnc/meltool/internal/selfupdate"
)

const appUpdateLong = `Meltool の新しいバージョンを GitHub のリリース (otnc/Meltool) から取得し、いま動いている meltool.exe を置き換えます。Meltype のバージョンは変えません。

既定は正式版の最新です。--pre を付けたときだけ、プレリリースも含めた最新を選びます。いまより古いバージョンには下げません。
配布物は GitHub が示す SHA-256 と照合してから置き換えます。前の meltool.exe は meltool.exe.old に退避し、次の更新で消します。`

func setupAppUpdateFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("pre", false, "プレリリースも含めて探す")
}

func runAppUpdate(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		flags := cmd.Flags()
		pre, _ := flags.GetBool("pre")
		dryRun, _ := flags.GetBool("dry-run")
		yes, _ := flags.GetBool("yes")
		asJSON, _ := flags.GetBool("json")
		if env.Version == "dev" || env.Version == "" {
			return errors.New("開発中のビルドなので、更新しません。リリースの meltool.exe を使ってください")
		}
		exe, err := env.Executable()
		if err != nil {
			return fmt.Errorf("meltool.exe の場所が分かりませんでした: %w", err)
		}

		c, err := newRemoteClient(env).Fetch(cmd.Context(), selfupdate.Repository, nil)
		if err != nil {
			return fmt.Errorf("Meltool のリリースの一覧を取得できませんでした: %w", err)
		}
		release, asset, ok := selfupdate.Select(c.Releases(), env.Version, pre, env.Platform.OS, env.Platform.Arch)
		out := cmd.OutOrStdout()
		if !ok {
			if asJSON {
				return writeJSON(out, map[string]any{"changed": false, "current": env.Version})
			}
			fmt.Fprintf(out, "Meltool %s は最新です。\n", env.Version)
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
		confirmer := prompt.Confirmer{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr(), Interactive: env.Interactive && !asJSON, AssumeYes: yes}
		if err := confirmer.Confirm(fmt.Sprintf("Meltool を %s に更新しますか？", release.Tag), prompt.DefaultYes); err != nil {
			return err
		}

		// 置き換えは rename で行うので、取得したものも同じディレクトリ (同じドライブ) に置く
		archive, err := os.CreateTemp(filepath.Dir(exe), "meltool-update-*.zip")
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
		fmt.Fprintf(out, "Meltool を %s に更新しました。\n", release.Tag)
		return nil
	}
}
