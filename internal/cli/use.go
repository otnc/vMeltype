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

const useLong = `保持バージョンを有効にします。通信しません。保持していないバージョンは、先に "vmeltype install <tag>" で取得します。

バージョンの指定は install と同じで、保持しているバージョンの中から選びます。バージョンを省くと保持している正式版の最新、1 や 1.4 ではその範囲の最新、--pre ではプレリリースの最新です。

portable 版は --portable を付けたときだけ対象にします。付けなければ通常版 (IME の登録を伴うもの) だけを探し、portable 版しか保持していなくても選びません。
いまは Windows だけに対応しています。どちらも実行先を固定し、バージョンを切り替えるときはアプリのファイルだけを入れ替えます。vMeltype が置いていないファイルはそのまま引き継ぎます。
portable 版の実行先は管理ルートの portable\app で、データは隣の data です。
通常版の実行先とデータ先は、本家と同じ %LOCALAPPDATA%\Programs\Meltype と %LOCALAPPDATA%\Meltype です。本家のインストールがあれば取り込み、置き換えた本家のファイルを管理前の記録として残します。切り替えの前にデータのスナップショットを作り、本家の自動更新を止め、本家がダウンロード済みの更新を管理ルートに移します。
Meltype IME を登録済みなら、実行先を入れ替える前に、IME の DLL も切り替え先のバージョンのものに入れ替えます。入れ替えには切り替え先のバージョンの本家のスクリプトを使い、DLL が違えば管理者権限の確認 (UAC) が出ます。断られたら切り替えません。登録していなければ、IME には触れません。
同じバージョンが有効で配置も一致していれば何もしません。--force を付けると、一致していても入れ替えます。

実行先の Meltype が動いていれば、正常に終了させてから切り替え、終わったら起動し直します。終了させるのは、--stop-app を付けたとき、-y を付けたとき (設定 stop-app-on-yes が true のとき)、対話で終了してよいと答えたときです。終わらなければ、強制終了せずに止めます。`

// errAppRunning は、実行先の Meltype が動いていて、切り替えられないことを表す。
var errAppRunning = errors.New("実行先の Meltype が動いています。Meltype を終了してから、もう一度実行してください")

func setupUseFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("reinstall", false, "--force と同じ")
	cmd.Flags().Bool("pre", false, "正式版ではなく、保持しているプレリリースから最新を選ぶ")
	cmd.Flags().String("id", "", "保持 ID で保持バージョンを選ぶ")
	cmd.Flags().Bool("portable", false, "portable 版を対象にする。付けなければ通常版を対象にする")
	addOverridePinFlag(cmd)
	addStopAppFlag(cmd)
}

func setupRollbackFlags(cmd *cobra.Command) {
	addOverridePinFlag(cmd)
	addStopAppFlag(cmd)
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
			return errors.New("直前に有効だったバージョンの記録がないので、戻せません")
		}
		st := store.Store{Root: root}
		target, err := st.Get(state.Previous.StoreID)
		if err != nil {
			return fmt.Errorf("直前のバージョン %s は保持していないので、戻せません: %w", state.Previous.StoreID, err)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "rollback はバージョンだけを戻し、data は戻しません。")
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
	place, err := layoutFor(env, root, target.Variant)
	if err != nil {
		return err
	}
	standard := target.Variant == string(assets.Standard)
	imeRegistered := false
	if standard {
		if imeRegistered, err = env.IMERegistered(); err != nil {
			return err
		}
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
	plan, err := lifecycle.PlanUse(root, place.appDir, target, lifecycle.UseOptions{OverridePin: overridePin, Adopt: standard})
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

	app, err := inspectApp(cmd, env, root, plan.AppDir)
	if err != nil {
		return err
	}
	planOut := out
	if asJSON {
		planOut = cmd.ErrOrStderr()
	}
	writeUsePlan(planOut, plan, place)
	if imeRegistered {
		fmt.Fprintln(planOut, "Meltype IME:  登録済みなので、DLL をこのバージョンのものに入れ替えます。DLL が違えば、管理者権限の確認 (UAC) が出ます。入力方式の一覧と既定の入力方式は変えません。")
	}
	app.describe(planOut)
	if dryRun {
		if asJSON {
			return writeJSON(out, map[string]any{"dryRun": true, "target": target.ID, "appDir": plan.AppDir, "dataDir": place.dataDir, "carried": nonNil(plan.Carried), "adopted": nonNil(plan.Adopted), "stopApp": app.needed()})
		}
		return nil
	}
	interactive := env.Interactive && !asJSON
	if err := app.confirmable(interactive); err != nil {
		return err
	}
	confirmer := prompt.Confirmer{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr(), Interactive: interactive, AssumeYes: app.assumeYes(yes)}
	if err := confirmer.Confirm(app.question(fmt.Sprintf(question, target.Tag)), prompt.DefaultYes); err != nil {
		return err
	}

	stopped, err := app.stop(env)
	if stopped {
		defer func() { restartApp(cmd, env, plan.AppDir, err) }()
	}
	if err != nil {
		return err
	}
	if standard {
		activeTag := ""
		if plan.From != nil {
			activeTag = plan.From.Tag
		}
		if err = lifecycle.PrepareStandardData(root, place.dataDir, activeTag, env.Now()); err != nil {
			return err
		}
		if imeRegistered {
			if err = replaceIME(env, filepath.Join(st.Path(target.ID), "files", "app"), target.Tag); err != nil {
				return err
			}
		}
	}
	if err = lifecycle.ExecuteUse(root, st.Path(target.ID), plan, env.Now()); err != nil {
		return err
	}
	if asJSON {
		return writeJSON(out, map[string]any{"changed": true, "active": target.ID, "appDir": plan.AppDir})
	}
	if stopped {
		fmt.Fprintf(out, "%s に切り替えました。\n", target.Tag)
		return nil
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
		description := fmt.Sprintf("%s から %s への切り替えが中断しています。新しいバージョンを置く前に止まったので、%s の実行先に戻します。", from, j.To.Tag, from)
		if plan.Complete {
			description = fmt.Sprintf("%s から %s への切り替えが中断しています。新しいバージョンはもう置いてあるので、data などの引き継ぎを終えて %s を有効にします。", from, j.To.Tag, j.To.Tag)
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
			return usageError{fmt.Errorf("uninstall では、削除するバージョンを省略せずに指定してください (例: 1.4.0、1.4.1-pre.1): %q", query.input)}
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
			return fmt.Errorf("%s は有効なバージョンなので削除できません。別のバージョンに切り替えてから削除してください", target.ID)
		}

		planOut := cmd.OutOrStdout()
		if asJSON {
			planOut = cmd.ErrOrStderr()
		}
		fmt.Fprintf(planOut, "削除する保持バージョン: %s (%s)\n", target.ID, st.Path(target.ID))
		fmt.Fprintln(planOut, "実行先、data、OS の登録には触れません。")
		if state.Previous != nil && state.Previous.StoreID == target.ID {
			fmt.Fprintln(planOut, "直前のバージョンなので、削除すると rollback で戻れなくなります。")
		}
		if dryRun {
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"dryRun": true, "removed": target.ID})
			}
			return nil
		}
		confirmer := prompt.Confirmer{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr(), Interactive: env.Interactive && !asJSON, AssumeYes: yes}
		if err := confirmer.Confirm(fmt.Sprintf("保持バージョン %s を削除しますか？", target.ID), prompt.DefaultNo); err != nil {
			return err
		}
		if err := st.Remove(target.ID); err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd.OutOrStdout(), map[string]any{"removed": target.ID})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "保持バージョン %s を削除しました。\n", target.ID)
		return nil
	}
}

func setupUninstallFlags(cmd *cobra.Command) {
	cmd.Flags().String("id", "", "保持 ID で保持バージョンを選ぶ")
	cmd.Flags().Bool("portable", false, "portable 版を対象にする。付けなければ通常版を対象にする")
}

// storedQuery は、保持バージョンを選ぶための指定。input は利用者が書いたバージョンの指定 (省略なら空)。
type storedQuery struct {
	input   string
	query   versions.Query
	pre     bool
	id      string
	variant assets.Variant
}

// newStoredQuery は、引数と --pre、--id、--portable から、保持バージョンの選び方を作る。
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

// resolveStored は、バージョンの指定か保持 ID から、この端末の保持バージョンを 1 つに決める。保持バージョンの manifest だけを使い、通信しない。
// variant の違う保持バージョンは、同じタグでも対象にしない。portable 版は --portable で明示したときだけ扱うため。
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
	// 同じ端末と variant の中では、タグごとに保持バージョンは 1 つになる
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

func writeUsePlan(w io.Writer, plan lifecycle.UsePlan, place layout) {
	from := "なし"
	if plan.From != nil {
		from = fmt.Sprintf("%s (%s)", plan.From.Tag, plan.From.StoreID)
	}
	fmt.Fprintf(w, "切り替え元:   %s\n", from)
	fmt.Fprintf(w, "切り替え先:   %s (%s)\n", plan.Target.Tag, plan.Target.ID)
	fmt.Fprintf(w, "実行先:       %s\n", plan.AppDir)
	if len(plan.Adopted) > 0 {
		fmt.Fprintf(w, "取り込むもの: %s (本家のインストール。置き換える本家のファイルは管理前の記録として残します)\n", summarize(plan.Adopted))
	}
	if len(plan.Carried) > 0 {
		fmt.Fprintf(w, "引き継ぐもの: %s\n", strings.Join(plan.Carried, "、"))
	}
	if place.variant == string(assets.Standard) {
		fmt.Fprintf(w, "データ先:     %s\n", place.dataDir)
		fmt.Fprintln(w, "切り替えの前にデータのスナップショットを作り、本家の自動更新を止めます。本家がダウンロード済みの更新は、管理ルートに移します。")
	}
	fmt.Fprintln(w, "データはそのまま使います。新しいバージョンを初めて起動したときに、Meltype がデータを移行することがあります。")
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
			fmt.Fprintln(out, "有効なバージョンはありません。\"vmeltype use <tag>\" で有効にします。")
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
