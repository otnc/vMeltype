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
	"github.com/otnc/vmeltype/internal/versions"
)

const useLong = `保持版を有効にします。通信しません。保持していない版は、先に "vmeltype install <tag>" で取得します。

版の指定は install と同じで、保持している版の中から選びます。版を省くと保持している正式版の最新、1 や 1.4 ではその範囲の最新、--pre ではプレリリースの最新です。

portable 版は --portable を付けたときだけ対象にします。付けなければ通常版 (IME の登録を伴うもの) だけを探し、portable 版しか保持していなくても選びません。
いまは Windows の portable 版だけに対応しています。portable 版は実行先 (管理ルートの portable\app) を固定し、版を切り替えるときはアプリのファイルだけを入れ替えます。隣の data と、vMeltype が置いていないファイルはそのまま引き継ぎます。
同じ版が有効で配置も一致していれば何もしません。--force を付けると、一致していても入れ替えます。`

// errAppRunning は、実行先の Meltype が動いていて、切り替えられないことを表す。
var errAppRunning = errors.New("実行先の Meltype が動いています。Meltype を終了してから、もう一度実行してください")

func setupUseFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("reinstall", false, "--force と同じ")
	cmd.Flags().Bool("pre", false, "正式版ではなく、保持しているプレリリースから最新を選ぶ")
	cmd.Flags().String("id", "", "保持 ID で保持版を選ぶ")
	cmd.Flags().Bool("portable", false, "portable 版を対象にする。付けなければ通常版を対象にする")
	addOverridePinFlag(cmd)
}

func runUse(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		force, _ := flags.GetBool("force")
		reinstall, _ := flags.GetBool("reinstall")
		query, err := newStoredQuery(cmd, args)
		if err != nil {
			return err
		}

		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		st := store.Store{Root: root}
		target, err := resolveStored(st, query, env.Platform)
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
		return fmt.Errorf("%s は通常版です。use はいまは portable 版だけに対応しています。portable 版を使うなら --portable を付けてください", target.ID)
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

	overridePin, _ := flags.GetBool("override-pin")
	plan, err := lifecycle.PlanPortableUse(root, target, overridePin)
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
		query, err := newStoredQuery(cmd, args)
		if err != nil {
			return err
		}
		if query.id == "" && !query.query.IsExact() {
			return usageError{fmt.Errorf("uninstall では、削除する版を省略せずに指定してください (例: 1.4.0、1.4.1-pre.1): %q", query.input)}
		}
		dryRun, _ := flags.GetBool("dry-run")
		yes, _ := flags.GetBool("yes")
		asJSON, _ := flags.GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		st := store.Store{Root: root}
		target, err := resolveStored(st, query, env.Platform)
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
	cmd.Flags().String("id", "", "保持 ID で保持版を選ぶ")
	cmd.Flags().Bool("portable", false, "portable 版を対象にする。付けなければ通常版を対象にする")
}

// storedQuery は、保持版を選ぶための指定。input は利用者が書いた版の指定 (省略なら空)。
type storedQuery struct {
	input   string
	query   versions.Query
	pre     bool
	id      string
	variant assets.Variant
}

// newStoredQuery は、引数と --pre、--id、--portable から、保持版の選び方を作る。
func newStoredQuery(cmd *cobra.Command, args []string) (storedQuery, error) {
	q := storedQuery{variant: selectedVariant(cmd)}
	if len(args) > 0 {
		q.input = args[0]
	}
	if cmd.Flags().Lookup("pre") != nil {
		q.pre, _ = cmd.Flags().GetBool("pre")
	}
	q.id, _ = cmd.Flags().GetString("id")
	query, err := versions.ParseQuery(q.input, q.pre)
	if err != nil {
		return storedQuery{}, usageError{err}
	}
	q.query = query
	return q, nil
}

func (q storedQuery) installHint() string {
	parts := []string{"vmeltype install"}
	if q.input != "" {
		parts = append(parts, q.input)
	}
	if q.pre {
		parts = append(parts, "--pre")
	}
	if q.variant == assets.Portable {
		parts = append(parts, "--portable")
	}
	return strings.Join(parts, " ")
}

// resolveStored は、版の指定か保持 ID から、この端末の保持版を 1 つに決める。保持版の manifest だけを使い、通信しない。
// variant の違う保持版は、同じタグでも対象にしない。portable 版は --portable で明示したときだけ扱うため。
func resolveStored(st store.Store, q storedQuery, platform assets.Platform) (store.Stored, error) {
	if q.id != "" {
		s, err := st.Get(q.id)
		if err != nil {
			return store.Stored{}, err
		}
		if q.input != "" {
			if _, ok := versions.Select([]versions.Candidate{{Tag: s.Tag, Prerelease: s.Prerelease}}, q.query); !ok {
				return store.Stored{}, usageError{fmt.Errorf("--id %s のタグは %s で、%s に合いません", q.id, s.Tag, q.query.Describe())}
			}
		}
		if s.Variant != string(q.variant) {
			return store.Stored{}, usageError{fmt.Errorf("--id %s は %s で、--portable の指定と合いません", q.id, variantLabels[s.Variant])}
		}
		return s, nil
	}

	stored, _, err := st.List()
	if err != nil {
		return store.Stored{}, err
	}
	// 同じ端末と variant の中では、タグごとに保持版は 1 つになる
	sameByTag := map[string]store.Stored{}
	var same, other []versions.Candidate
	for _, s := range stored {
		if s.OS != platform.OS || s.Arch != platform.Arch {
			continue
		}
		c := versions.Candidate{Tag: s.Tag, Prerelease: s.Prerelease}
		if s.Variant == string(q.variant) {
			sameByTag[s.Tag] = s
			same = append(same, c)
		} else {
			other = append(other, c)
		}
	}
	if chosen, ok := versions.Select(same, q.query); ok {
		return sameByTag[chosen.Tag], nil
	}

	wanted := q.query.Describe()
	_, otherMatches := versions.Select(other, q.query)
	switch {
	case otherMatches && q.variant == assets.Standard:
		return store.Stored{}, fmt.Errorf("%s の通常版は保持していません。portable 版を対象にするなら --portable を付けてください", wanted)
	case otherMatches:
		return store.Stored{}, fmt.Errorf("%s の portable 版は保持していません。通常版を対象にするなら --portable を外してください", wanted)
	default:
		return store.Stored{}, fmt.Errorf("%s は保持していません。先に \"%s\" を実行してください", wanted, q.installHint())
	}
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
			return writeJSON(out, map[string]any{"active": state.Active, "previous": state.Previous, "pin": state.Pin})
		}
		if state.Active == nil {
			fmt.Fprintln(out, "有効な版はありません。\"vmeltype use <tag>\" で有効にします。")
			return nil
		}
		variant := variantLabels[state.Active.Variant]
		fmt.Fprintf(out, "%s (%s、保持 ID: %s)\n", state.Active.Tag, variant, state.Active.StoreID)
		fmt.Fprintf(out, "実行先: %s\n", state.Active.AppDir)
		if state.Pin != nil {
			fmt.Fprintf(out, "固定: %s (%s)\n", state.Pin.Tag, state.Pin.StoreID)
		}
		return nil
	}
}
