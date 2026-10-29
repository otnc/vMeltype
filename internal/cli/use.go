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
		id, _ := flags.GetString("id")
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
		return switchVersion(cmd, env, root, st, target, force || reinstall, "%s に切り替えますか？")
	}
}

func runRollback(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		state, err := lifecycle.LoadState(root)
		if err != nil {
			return err
		}
		if state.Previous == nil {
			return errors.New("直前に有効だった版の記録がないので、戻せません")
		}
		st := store.Store{Root: root}
		target, err := st.Get(state.Previous.StoreID)
		if err != nil {
			return fmt.Errorf("直前の版 %s は保持していないので、戻せません: %w", state.Previous.StoreID, err)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "rollback は版だけを戻し、data は戻しません。")
		return switchVersion(cmd, env, root, st, target, false, "%s に戻しますか？")
	}
}

// switchVersion は、use と rollback に共通の切り替え。計画を示して確認してから、実行先を入れ替える。
// question は、確認の文言で、%s に切り替え先のタグが入る。
func switchVersion(cmd *cobra.Command, env *Env, root string, st store.Store, target store.Stored, force bool, question string) error {
	flags := cmd.Flags()
	dryRun, _ := flags.GetBool("dry-run")
	yes, _ := flags.GetBool("yes")
	asJSON, _ := flags.GetBool("json")
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
	if err := confirmer.Confirm(fmt.Sprintf(question, target.Tag), prompt.DefaultYes); err != nil {
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

func runRecover(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		asJSON, _ := cmd.Flags().GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		lock, err := lifecycle.AcquireLock(root)
		if err != nil {
			return err
		}
		defer lock.Release()

		plan, err := lifecycle.PlanRecover(root)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if plan == nil {
			if asJSON {
				return writeJSON(out, map[string]any{"pending": false})
			}
			fmt.Fprintln(out, "中断した変更はありません。")
			return nil
		}
		j := plan.Journal
		from := "なし"
		if j.From != nil {
			from = j.From.Tag
		}
		description := fmt.Sprintf("%s から %s への切り替えが中断しています。新しい版を置く前に止まったので、%s の実行先に戻します。", from, j.To.Tag, from)
		if plan.Complete {
			description = fmt.Sprintf("%s から %s への切り替えが中断しています。新しい版はもう置いてあるので、data などの引き継ぎを終えて %s を有効にします。", from, j.To.Tag, j.To.Tag)
		}
		planOut := out
		if asJSON {
			planOut = cmd.ErrOrStderr()
		}
		fmt.Fprintln(planOut, description)
		if dryRun {
			if asJSON {
				return writeJSON(out, map[string]any{"pending": true, "dryRun": true, "complete": plan.Complete, "to": j.To.StoreID})
			}
			return nil
		}
		confirmer := prompt.Confirmer{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr(), Interactive: env.Interactive && !asJSON, AssumeYes: yes}
		if err := confirmer.Confirm("復旧しますか？", prompt.DefaultYes); err != nil {
			return err
		}
		if err := lifecycle.ExecuteRecover(root, plan); err != nil {
			return err
		}
		if asJSON {
			return writeJSON(out, map[string]any{"pending": false, "complete": plan.Complete, "to": j.To.StoreID})
		}
		fmt.Fprintln(out, "復旧しました。")
		return nil
	}
}

func runUninstall(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		id, _ := flags.GetString("id")
		dryRun, _ := flags.GetBool("dry-run")
		yes, _ := flags.GetBool("yes")
		asJSON, _ := flags.GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		st := store.Store{Root: root}
		target, err := resolveStored(st, args, id, env.Platform)
		if err != nil {
			return err
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
		state, err := lifecycle.LoadState(root)
		if err != nil {
			return err
		}
		if state.Active != nil && state.Active.StoreID == target.ID {
			return fmt.Errorf("%s は有効な版なので削除できません。別の版に切り替えてから削除してください", target.ID)
		}

		planOut := cmd.OutOrStdout()
		if asJSON {
			planOut = cmd.ErrOrStderr()
		}
		fmt.Fprintf(planOut, "削除する保持版: %s (%s)\n", target.ID, st.Path(target.ID))
		fmt.Fprintln(planOut, "実行先、data、OS の登録には触れません。")
		if state.Previous != nil && state.Previous.StoreID == target.ID {
			fmt.Fprintln(planOut, "直前の版なので、削除すると rollback で戻れなくなります。")
		}
		if dryRun {
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"dryRun": true, "removed": target.ID})
			}
			return nil
		}
		confirmer := prompt.Confirmer{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr(), Interactive: env.Interactive && !asJSON, AssumeYes: yes}
		if err := confirmer.Confirm(fmt.Sprintf("保持版 %s を削除しますか？", target.ID), prompt.DefaultNo); err != nil {
			return err
		}
		if err := st.Remove(target.ID); err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd.OutOrStdout(), map[string]any{"removed": target.ID})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "保持版 %s を削除しました。\n", target.ID)
		return nil
	}
}

func setupUninstallFlags(cmd *cobra.Command) {
	cmd.Flags().String("id", "", "同じタグの保持版が複数あるときに、保持 ID で選ぶ")
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
