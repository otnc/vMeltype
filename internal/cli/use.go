package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otnc/vmeltype/internal/assets"
	"github.com/otnc/vmeltype/internal/lifecycle"
	"github.com/otnc/vmeltype/internal/prompt"
	"github.com/otnc/vmeltype/internal/store"
)

const useLong = `保持版を有効にします。通信しません。保持していない版は、先に "vmeltype install <tag>" で取得します。

いまは Windows の portable 版だけに対応しています。portable 版は実行先 (管理ルートの portable\app) を固定し、版を切り替えるときはアプリのファイルだけを入れ替えます。隣の data と、vMeltype が置いていないファイルはそのまま引き継ぎます。
同じ版が有効で配置も一致していれば何もしません。--force を付けると、一致していても入れ替えます。`

// errAppRunning は、実行先の Meltype が動いていて、切り替えられないことを表す。
var errAppRunning = errors.New("実行先の Meltype が動いています。Meltype を終了してから、もう一度実行してください")

func setupUseFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("reinstall", false, "--force と同じ")
	cmd.Flags().String("id", "", "同じタグの保持版が複数あるときに、保持 ID で選ぶ")
}

func runUse(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		force, _ := flags.GetBool("force")
		reinstall, _ := flags.GetBool("reinstall")
		force = force || reinstall
		id, _ := flags.GetString("id")
		dryRun, _ := flags.GetBool("dry-run")
		yes, _ := flags.GetBool("yes")
		asJSON, _ := flags.GetBool("json")
		if len(args) == 0 && id == "" {
			return usageError{errors.New("版を指定してください (例: vmeltype use v1.4.0)")}
		}

		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		st := store.Store{Root: root}
		target, err := resolveStored(st, args, id, env.Platform)
		if err != nil {
			return err
		}
		if target.Variant != string(assets.Portable) {
			return fmt.Errorf("%s は通常版です。use はいまは portable 版だけに対応しています (\"vmeltype install %s --variant portable\" で取得できます)", target.ID, target.Tag)
		}

		lock, err := lifecycle.AcquireLock(root)
		if err != nil {
			return err
		}
		defer lock.Release()
		if journal, err := lifecycle.LoadJournal(root); err != nil {
			return err
		} else if journal != nil {
			return lifecycle.ErrPendingJournal
		}

		plan, err := lifecycle.PlanPortableUse(root, target)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if plan.UpToDate && !force {
			if asJSON {
				return writeJSON(out, map[string]any{"changed": false, "active": target.ID})
			}
			fmt.Fprintf(out, "既に %s が有効で、実行先も一致しています。入れ替えるには --force を付けます。\n", target.Tag)
			return nil
		}

		planOut := out
		if asJSON {
			planOut = cmd.ErrOrStderr()
		}
		writeUsePlan(planOut, plan)
		if dryRun {
			if asJSON {
				return writeJSON(out, map[string]any{"dryRun": true, "target": target.ID, "appDir": plan.AppDir, "carried": nonNil(plan.Carried)})
			}
			return nil
		}
		confirmer := prompt.Confirmer{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr(), Interactive: env.Interactive && !asJSON, AssumeYes: yes}
		if err := confirmer.Confirm(fmt.Sprintf("%s に切り替えますか？", target.Tag), prompt.DefaultYes); err != nil {
			return err
		}

		// 確認を待つ間に起動されることがあるので、入れ替える直前に確かめる
		running, err := env.RunningUnder(plan.AppDir)
		if err != nil {
			return fmt.Errorf("実行先の Meltype が動いているかを確かめられませんでした: %w", err)
		}
		if len(running) > 0 {
			return fmt.Errorf("%w (%s)", errAppRunning, strings.Join(running, "、"))
		}

		if err := lifecycle.ExecutePortableUse(root, st.Path(target.ID), plan, env.Now()); err != nil {
			return err
		}
		if asJSON {
			return writeJSON(out, map[string]any{"changed": true, "active": target.ID, "appDir": plan.AppDir})
		}
		fmt.Fprintf(out, "%s に切り替えました。起動するには %s を実行します。\n", target.Tag, filepath.Join(plan.AppDir, "Meltype.exe"))
		return nil
	}
}

// resolveStored は、タグか保持 ID から、この端末の保持版を 1 つに決める。保持版の manifest だけを使い、通信しない。
func resolveStored(st store.Store, args []string, id string, platform assets.Platform) (store.Stored, error) {
	if id != "" {
		s, err := st.Get(id)
		if err != nil {
			return store.Stored{}, err
		}
		if len(args) > 0 && !sameTag(s.Tag, args[0]) {
			return store.Stored{}, usageError{fmt.Errorf("--id %s のタグは %s で、%s ではありません", id, s.Tag, args[0])}
		}
		return s, nil
	}

	stored, _, err := st.List()
	if err != nil {
		return store.Stored{}, err
	}
	var exact, alternative []store.Stored
	for _, s := range stored {
		if s.OS != platform.OS || s.Arch != platform.Arch {
			continue
		}
		switch {
		case s.Tag == args[0]:
			exact = append(exact, s)
		case sameTag(s.Tag, args[0]):
			alternative = append(alternative, s)
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = alternative
	}
	switch len(candidates) {
	case 0:
		return store.Stored{}, fmt.Errorf("%s は保持していません。先に \"vmeltype install %s\" を実行してください", args[0], args[0])
	case 1:
		return candidates[0], nil
	default:
		var ids []string
		for _, c := range candidates {
			ids = append(ids, c.ID)
		}
		return store.Stored{}, usageError{fmt.Errorf("%s の保持版が複数あります。--id で選んでください: %s", args[0], strings.Join(ids, "、"))}
	}
}

// sameTag は、先頭の v の有無だけが違うタグを同じとみなす。
func sameTag(a, b string) bool {
	return strings.TrimPrefix(a, "v") == strings.TrimPrefix(b, "v")
}

func writeUsePlan(w io.Writer, plan lifecycle.UsePlan) {
	from := "なし"
	if plan.From != nil {
		from = fmt.Sprintf("%s (%s)", plan.From.Tag, plan.From.StoreID)
	}
	fmt.Fprintf(w, "切り替え元:   %s\n", from)
	fmt.Fprintf(w, "切り替え先:   %s (%s)\n", plan.Target.Tag, plan.Target.ID)
	fmt.Fprintf(w, "実行先:       %s\n", plan.AppDir)
	if len(plan.Carried) > 0 {
		fmt.Fprintf(w, "引き継ぐもの: %s\n", strings.Join(plan.Carried, "、"))
	}
	fmt.Fprintln(w, "data はそのまま使います。新しい版を初めて起動したときに、Meltype がデータを移行することがあります。")
}

func runCurrent(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		state, err := lifecycle.LoadState(root)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			return writeJSON(out, map[string]any{"active": state.Active, "previous": state.Previous})
		}
		if state.Active == nil {
			fmt.Fprintln(out, "有効な版はありません。\"vmeltype use <tag>\" で有効にします。")
			return nil
		}
		variant := variantLabels[state.Active.Variant]
		fmt.Fprintf(out, "%s (%s、保持 ID: %s)\n", state.Active.Tag, variant, state.Active.StoreID)
		fmt.Fprintf(out, "実行先: %s\n", state.Active.AppDir)
		return nil
	}
}
