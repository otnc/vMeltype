package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otnc/vmeltype/internal/assets"
	"github.com/otnc/vmeltype/internal/config"
	"github.com/otnc/vmeltype/internal/lifecycle"
	"github.com/otnc/vmeltype/internal/prompt"
	"github.com/otnc/vmeltype/internal/snapshot"
)

const snapshotLong = `Meltype のデータを保存し、検査し、復元します。通信しません。

有効なバージョンのデータ先 (通常版は %LOCALAPPDATA%\Meltype、portable 版は管理ルートの portable\app\data) を扱います。
本家のバックアップと違い、未知のものも含めてデータ先の通常のファイルをすべて保存し、各ファイルの SHA-256 を記録します。
通常版の切り替えの前と、復元の前には自動で作ります。自動で作ったものは、新しいほうから設定 auto-snapshot-keep の数 (既定は 10) だけ残し、古いものを消します。手動で作ったものと取り込んだものは消しません。`

var reasonLabels = map[snapshot.Reason]string{
	snapshot.ReasonManual:        "手動",
	snapshot.ReasonBeforeRestore: "復元の前",
	snapshot.ReasonBeforeUse:     "切り替えの前",
}

// portableDataDir は、portable 版の data の場所を返す。本家の portable 版は、Meltype.exe の隣の data を使う。
func portableDataDir(root string) string {
	return filepath.Join(lifecycle.PortableAppDir(root), "data")
}

// lockForDataChange は、管理ルートを排他し、未完了の切り替えがあれば止める。
func lockForDataChange(root string) (*lifecycle.Lock, error) {
	lock, err := lifecycle.AcquireLock(root)
	if err != nil {
		return nil, err
	}
	journal, err := lifecycle.LoadJournal(root)
	if err == nil && journal != nil {
		err = lifecycle.ErrPendingJournal
	}
	if err != nil {
		lock.Release()
		return nil, err
	}
	return lock, nil
}

func activeTag(root string) string {
	state, err := lifecycle.LoadState(root)
	if err != nil || state.Active == nil {
		return ""
	}
	return state.Active.Tag
}

func runSnapshotCreate(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		asJSON, _ := cmd.Flags().GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		place, err := activeLayout(env, root)
		if err != nil {
			return err
		}
		dataDir, appDir := place.dataDir, place.appDir
		app, err := inspectApp(cmd, env, root, appDir)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if dryRun {
			if asJSON {
				return writeJSON(out, map[string]any{"dryRun": true, "source": dataDir, "stopApp": app.needed()})
			}
			fmt.Fprintf(out, "%s を保存します。\n", dataDir)
			app.describe(out)
			return nil
		}
		// 保存は何も失わないので確認しないが、Meltype を終了させるなら聞く
		if app.needed() && !app.consented {
			interactive := env.Interactive && !asJSON
			if err := app.confirmable(interactive); err != nil {
				return err
			}
			confirmer := env.confirmer(cmd, interactive, false)
			if err := confirmer.Confirm(app.question("data を保存しますか？"), prompt.DefaultYes); err != nil {
				return err
			}
		}

		lock, err := lockForDataChange(root)
		if err != nil {
			return err
		}
		defer lock.Release()
		stopped, err := app.stop(env)
		if stopped {
			defer func() { restartApp(cmd, env, appDir, err) }()
		}
		if err != nil {
			return err
		}
		m, err := snapshot.Create(root, dataDir, snapshot.Meta{Reason: snapshot.ReasonManual, ActiveTag: activeTag(root)}, env.Now())
		if errors.Is(err, snapshot.ErrNoData) {
			return fmt.Errorf("portable 版の data (%s) がないので、保存するものがありません", dataDir)
		}
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(out, m)
		}
		fmt.Fprintf(out, "スナップショット %s を作りました (%d ファイル、%s)。\n", m.ID, len(m.Files), formatBytes(m.TotalBytes))
		return nil
	}
}

func runSnapshotList(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		listed, problems, err := snapshot.List(root)
		if err != nil {
			return err
		}
		for _, p := range problems {
			fmt.Fprintf(cmd.ErrOrStderr(), "警告: スナップショット %s を読めません: %v\n", p.ID, p.Err)
		}
		out := cmd.OutOrStdout()
		if asJSON {
			return writeJSON(out, map[string]any{"snapshots": nonNil(listed)})
		}
		if len(listed) == 0 {
			fmt.Fprintln(out, "スナップショットはありません。\"vmeltype snapshot create\" で作ります。")
			return nil
		}
		rows := [][]string{{"ID", "作った日時", "理由", "有効だったバージョン", "ファイル", "サイズ"}}
		for _, m := range listed {
			tag := m.ActiveTag
			if tag == "" {
				tag = "-"
			}
			reason := reasonLabels[m.Reason]
			if m.ImportedAt != nil {
				reason += " (取り込み)"
			}
			rows = append(rows, []string{m.ID, formatLocal(m.CreatedAt), reason, tag, strconv.Itoa(len(m.Files)) + " 件", formatBytes(m.TotalBytes)})
		}
		writeTable(out, rows)
		return nil
	}
}

func runSnapshotInspect(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		m, err := snapshot.Get(root, args[0])
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if asJSON {
			return writeJSON(out, m)
		}
		writeSnapshotSummary(out, m)
		fmt.Fprintln(out)
		for _, f := range m.Files {
			fmt.Fprintf(out, "  %s (%s)\n", f.Path, formatBytes(f.Size))
		}
		return nil
	}
}

func writeSnapshotSummary(w io.Writer, m snapshot.Manifest) {
	fmt.Fprintf(w, "スナップショット:     %s\n", m.ID)
	fmt.Fprintf(w, "作った日時:           %s (%s)\n", formatLocal(m.CreatedAt), reasonLabels[m.Reason])
	if m.ImportedAt != nil {
		fmt.Fprintf(w, "取り込んだ日時:       %s\n", formatLocal(*m.ImportedAt))
	}
	if m.ActiveTag != "" {
		fmt.Fprintf(w, "有効だったバージョン: %s\n", m.ActiveTag)
	}
	fmt.Fprintf(w, "保存元:               %s\n", m.Source)
	fmt.Fprintf(w, "中身:                 %d ファイル、%s\n", len(m.Files), formatBytes(m.TotalBytes))
}

func runSnapshotVerify(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		if err := snapshot.Verify(root, args[0]); err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd.OutOrStdout(), map[string]any{"id": args[0], "ok": true})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "スナップショット %s は記録と一致しています。\n", args[0])
		return nil
	}
}

func runSnapshotRestore(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		dryRun, _ := flags.GetBool("dry-run")
		yes, _ := flags.GetBool("yes")
		asJSON, _ := flags.GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		id := args[0]
		if err := snapshot.Verify(root, id); err != nil {
			return err
		}
		m, err := snapshot.Get(root, id)
		if err != nil {
			return err
		}
		place, err := activeLayout(env, root)
		if err != nil {
			return err
		}
		dataDir := place.dataDir
		if from, to := dataKind(m.Source), dataKind(dataDir); from != to {
			return dataKindMismatch(root, id, m.Source, from)
		}

		out := cmd.OutOrStdout()
		planOut := out
		if asJSON {
			planOut = cmd.ErrOrStderr()
		}
		appDir := place.appDir
		app, err := inspectApp(cmd, env, root, appDir)
		if err != nil {
			return err
		}
		writeSnapshotSummary(planOut, m)
		fmt.Fprintf(planOut, "復元先:               %s\n", dataDir)
		fmt.Fprintln(planOut, "いまのデータは、置き換える前に別のスナップショットとして保存します。")
		app.describe(planOut)
		if dryRun {
			if asJSON {
				return writeJSON(out, map[string]any{"dryRun": true, "id": id, "destination": dataDir, "stopApp": app.needed()})
			}
			return nil
		}
		interactive := env.Interactive && !asJSON
		if err := app.confirmable(interactive); err != nil {
			return err
		}
		confirmer := env.confirmer(cmd, interactive, app.assumeYes(yes))
		if err := confirmer.Confirm(app.question(fmt.Sprintf("data を %s の時点に戻しますか？", id)), prompt.DefaultNo); err != nil {
			return err
		}

		lock, err := lockForDataChange(root)
		if err != nil {
			return err
		}
		defer lock.Release()
		stopped, err := app.stop(env)
		if stopped {
			defer func() { restartApp(cmd, env, appDir, err) }()
		}
		if err != nil {
			return err
		}
		before, err := snapshot.Create(root, dataDir, snapshot.Meta{Reason: snapshot.ReasonBeforeRestore, ActiveTag: activeTag(root)}, env.Now())
		if err != nil && !errors.Is(err, snapshot.ErrNoData) {
			return fmt.Errorf("いまの data を保存できなかったので、復元しません: %w", err)
		}
		if err := snapshot.Restore(root, id, dataDir); err != nil {
			return err
		}
		pruneAutoSnapshots(cmd, root, id)
		if asJSON {
			return writeJSON(out, map[string]any{"restored": id, "before": before.ID})
		}
		fmt.Fprintf(out, "data を %s の時点に戻しました。", id)
		if before.ID != "" {
			fmt.Fprintf(out, "戻す前の data は %s に保存しています。", before.ID)
		}
		fmt.Fprintln(out)
		return nil
	}
}

func runSnapshotExport(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		id, file := args[0], args[1]
		if filepath.Ext(file) == "" {
			file += snapshot.FileExtension
		}
		if err := snapshot.Export(root, id, file); err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			return writeJSON(out, map[string]any{"id": id, "file": file})
		}
		fmt.Fprintf(out, "スナップショット %s を %s に書き出しました。\n", id, file)
		fmt.Fprintln(out, "このファイルには、Meltype の設定、辞書、学習の中身が入っています。人に渡したり公開したりしないでください。")
		fmt.Fprintf(out, "別の PC では \"vmeltype snapshot import %s\" で取り込みます。\n", filepath.Base(file))
		return nil
	}
}

func runSnapshotImport(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		m, err := snapshot.Import(root, args[0], env.Now())
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			return writeJSON(out, m)
		}
		fmt.Fprintf(out, "スナップショット %s として取り込みました (%d ファイル、%s)。\n", m.ID, len(m.Files), formatBytes(m.TotalBytes))
		fmt.Fprintf(out, "復元するには \"vmeltype snapshot restore %s\" を実行します。\n", m.ID)
		return nil
	}
}

// dataKind は、データ先が portable 版のもの (実行先の隣の data) か、通常版のものかを返す。
// 別の PC で作ったスナップショットはデータ先のパスが違うので、パスではなく種類で比べる。
func runSnapshotRemove(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		dryRun, _ := flags.GetBool("dry-run")
		yes, _ := flags.GetBool("yes")
		asJSON, _ := flags.GetBool("json")
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		m, err := snapshot.Get(root, args[0])
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		planOut := out
		if asJSON {
			planOut = cmd.ErrOrStderr()
		}
		fmt.Fprintf(planOut, "削除するスナップショット: %s\n", describeSnapshot(m))
		if dryRun {
			if asJSON {
				return writeJSON(out, map[string]any{"dryRun": true, "remove": []string{m.ID}})
			}
			return nil
		}
		confirmer := env.confirmer(cmd, env.Interactive && !asJSON, yes)
		if err := confirmer.Confirm(fmt.Sprintf("スナップショット %s を削除しますか？", m.ID), prompt.DefaultNo); err != nil {
			return err
		}
		lock, err := lifecycle.AcquireLock(root)
		if err != nil {
			return err
		}
		defer lock.Release()
		if err := snapshot.Remove(root, m.ID); err != nil {
			return err
		}
		if asJSON {
			return writeJSON(out, map[string]any{"removed": []string{m.ID}})
		}
		fmt.Fprintf(out, "スナップショット %s を削除しました。\n", m.ID)
		return nil
	}
}

func runSnapshotPrune(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		flags := cmd.Flags()
		dryRun, _ := flags.GetBool("dry-run")
		yes, _ := flags.GetBool("yes")
		asJSON, _ := flags.GetBool("json")
		keep, _ := flags.GetInt("keep")
		if flags.Changed("keep") && keep < 1 {
			return usageError{fmt.Errorf("--keep には 1 以上の数を指定してください: %d", keep)}
		}
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		if !flags.Changed("keep") {
			settings, err := config.Load(root)
			if err != nil {
				return err
			}
			keep = settings.AutoSnapshotKeep
		}
		out := cmd.OutOrStdout()
		planOut := out
		if asJSON {
			planOut = cmd.ErrOrStderr()
		}
		if keep == 0 {
			fmt.Fprintln(planOut, "設定 auto-snapshot-keep が 0 なので、消しません。消すなら --keep で残す数を指定します。")
			if asJSON {
				return writeJSON(out, map[string]any{"removed": []string{}})
			}
			return nil
		}
		targets, err := snapshot.PruneTargets(root, keep)
		if err != nil {
			return err
		}
		ids := []string{}
		for _, m := range targets {
			ids = append(ids, m.ID)
		}
		if len(targets) == 0 {
			fmt.Fprintf(planOut, "自動で作ったスナップショットは %d 個以下なので、消すものはありません。\n", keep)
			if asJSON {
				return writeJSON(out, map[string]any{"removed": ids})
			}
			return nil
		}
		fmt.Fprintf(planOut, "自動で作ったスナップショットのうち、新しい %d 個より古い次のものを消します。手動で作ったものと取り込んだものは消しません。\n", keep)
		for _, m := range targets {
			fmt.Fprintf(planOut, "  %s\n", describeSnapshot(m))
		}
		if dryRun {
			if asJSON {
				return writeJSON(out, map[string]any{"dryRun": true, "remove": ids})
			}
			return nil
		}
		confirmer := env.confirmer(cmd, env.Interactive && !asJSON, yes)
		if err := confirmer.Confirm(fmt.Sprintf("%d 個のスナップショットを削除しますか？", len(targets)), prompt.DefaultNo); err != nil {
			return err
		}
		lock, err := lifecycle.AcquireLock(root)
		if err != nil {
			return err
		}
		defer lock.Release()
		removed, err := snapshot.Prune(root, keep)
		if err != nil {
			return err
		}
		ids = []string{}
		for _, m := range removed {
			ids = append(ids, m.ID)
		}
		if asJSON {
			return writeJSON(out, map[string]any{"removed": ids})
		}
		fmt.Fprintf(out, "%d 個のスナップショットを削除しました。\n", len(removed))
		return nil
	}
}

// describeSnapshot は、スナップショットを 1 行で示す。
func describeSnapshot(m snapshot.Manifest) string {
	reason := reasonLabels[m.Reason]
	if m.ImportedAt != nil {
		reason += "、取り込み"
	}
	return fmt.Sprintf("%s (%s、%s)", m.ID, formatLocal(m.CreatedAt), reason)
}

// pruneAutoSnapshots は、自動で作ったスナップショットのうち、設定の数より古いものを消す。protected (いま復元したものなど) は消さない。
// 呼び出し元の操作はもう終わっているので、消せなくても失敗にはせず、警告だけを出す。
func pruneAutoSnapshots(cmd *cobra.Command, root string, protected ...string) {
	warn := cmd.ErrOrStderr()
	settings, err := config.Load(root)
	if err != nil {
		fmt.Fprintf(warn, "警告: 設定を読めないので、古い自動のスナップショットを消しませんでした: %v\n", err)
		return
	}
	removed, err := snapshot.Prune(root, settings.AutoSnapshotKeep, protected...)
	if len(removed) > 0 {
		fmt.Fprintf(warn, "自動で作ったスナップショットが %d 個を超えたので、古いものを %d 個消しました。残す数は \"vmeltype app config set auto-snapshot-keep <数>\" で変えられます。\n", settings.AutoSnapshotKeep, len(removed))
	}
	if err != nil {
		fmt.Fprintf(warn, "警告: 古い自動のスナップショットを消せませんでした: %v\n", err)
	}
}

// dataKindMismatch は、スナップショットのデータの種類 (通常版か portable 版か) が、いまの戻し先と違うことを表すエラーを返す。
// 有効なバージョンがなければ戻し先は portable 版の data になるので、通常版のデータは、通常版を有効にしてから戻すよう案内する。
func dataKindMismatch(root, id, source, kind string) error {
	if kind == string(assets.Portable) {
		return fmt.Errorf("スナップショット %s は portable 版のデータ (%s) で、いまの有効なバージョンは通常版なので、復元しません", id, source)
	}
	state, err := lifecycle.LoadState(root)
	if err != nil {
		return err
	}
	if state.Active == nil {
		return fmt.Errorf("スナップショット %s は通常版のデータ (%s) ですが、有効なバージョンがないので、戻し先を決められません。先に \"vmeltype use <tag>\" で通常版を有効にしてください", id, source)
	}
	return fmt.Errorf("スナップショット %s は通常版のデータ (%s) で、いまの有効なバージョンは portable 版なので、復元しません", id, source)
}

func dataKind(path string) string {
	clean := filepath.Clean(path)
	if strings.EqualFold(filepath.Base(clean), "data") &&
		strings.EqualFold(filepath.Base(filepath.Dir(clean)), "app") &&
		strings.EqualFold(filepath.Base(filepath.Dir(filepath.Dir(clean))), "portable") {
		return string(assets.Portable)
	}
	return string(assets.Standard)
}
