package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otnc/vmeltype/internal/lifecycle"
	"github.com/otnc/vmeltype/internal/prompt"
	"github.com/otnc/vmeltype/internal/snapshot"
)

const snapshotLong = `Meltype のデータを保存し、検査し、復元します。通信しません。

いまは portable 版のデータ (管理ルートの portable\app\data) だけを扱います。
本家のバックアップと違い、未知のものも含めて data の通常のファイルをすべて保存し、各ファイルの SHA-256 を記録します。`

var reasonLabels = map[snapshot.Reason]string{
	snapshot.ReasonManual:        "手動",
	snapshot.ReasonBeforeRestore: "復元の前",
}

// portableDataDir は、portable 版の data の場所を返す。本家の portable 版は、Meltype.exe の隣の data を使う。
func portableDataDir(root string) string {
	return filepath.Join(lifecycle.PortableAppDir(root), "data")
}

// ensureMeltypeStopped は、保存の途中で Meltype が書き込まないよう、実行先から動いていないことを確かめる。
func ensureMeltypeStopped(env *Env, root string) error {
	running, err := env.RunningUnder(lifecycle.PortableAppDir(root))
	if err != nil {
		return fmt.Errorf("実行先の Meltype が動いているかを確かめられませんでした: %w", err)
	}
	if len(running) > 0 {
		return fmt.Errorf("%w (%s)", errAppRunning, strings.Join(running, "、"))
	}
	return nil
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
		dataDir := portableDataDir(root)
		out := cmd.OutOrStdout()
		if dryRun {
			if asJSON {
				return writeJSON(out, map[string]any{"dryRun": true, "source": dataDir})
			}
			fmt.Fprintf(out, "%s を保存します。\n", dataDir)
			return nil
		}

		lock, err := lockForDataChange(root)
		if err != nil {
			return err
		}
		defer lock.Release()
		if err := ensureMeltypeStopped(env, root); err != nil {
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
		rows := [][]string{{"ID", "作った日時", "理由", "有効だった版", "ファイル", "サイズ"}}
		for _, m := range listed {
			tag := m.ActiveTag
			if tag == "" {
				tag = "-"
			}
			rows = append(rows, []string{m.ID, formatLocal(m.CreatedAt), reasonLabels[m.Reason], tag, strconv.Itoa(len(m.Files)) + " 件", formatBytes(m.TotalBytes)})
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
	fmt.Fprintf(w, "スナップショット: %s\n", m.ID)
	fmt.Fprintf(w, "作った日時:       %s (%s)\n", formatLocal(m.CreatedAt), reasonLabels[m.Reason])
	if m.ActiveTag != "" {
		fmt.Fprintf(w, "有効だった版:     %s\n", m.ActiveTag)
	}
	fmt.Fprintf(w, "保存元:           %s\n", m.Source)
	fmt.Fprintf(w, "中身:             %d ファイル、%s\n", len(m.Files), formatBytes(m.TotalBytes))
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
		dataDir := portableDataDir(root)

		out := cmd.OutOrStdout()
		planOut := out
		if asJSON {
			planOut = cmd.ErrOrStderr()
		}
		writeSnapshotSummary(planOut, m)
		fmt.Fprintf(planOut, "復元先:           %s\n", dataDir)
		fmt.Fprintln(planOut, "いまの data は、置き換える前に別のスナップショットとして保存します。")
		if dryRun {
			if asJSON {
				return writeJSON(out, map[string]any{"dryRun": true, "id": id, "destination": dataDir})
			}
			return nil
		}
		confirmer := prompt.Confirmer{In: cmd.InOrStdin(), Out: cmd.ErrOrStderr(), Interactive: env.Interactive && !asJSON, AssumeYes: yes}
		if err := confirmer.Confirm(fmt.Sprintf("data を %s の時点に戻しますか？", id), prompt.DefaultNo); err != nil {
			return err
		}

		lock, err := lockForDataChange(root)
		if err != nil {
			return err
		}
		defer lock.Release()
		if err := ensureMeltypeStopped(env, root); err != nil {
			return err
		}
		before, err := snapshot.Create(root, dataDir, snapshot.Meta{Reason: snapshot.ReasonBeforeRestore, ActiveTag: activeTag(root)}, env.Now())
		if err != nil && !errors.Is(err, snapshot.ErrNoData) {
			return fmt.Errorf("いまの data を保存できなかったので、復元しません: %w", err)
		}
		if err := snapshot.Restore(root, id, dataDir); err != nil {
			return err
		}
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
