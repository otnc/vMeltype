package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/otnc/vmeltype/internal/store"
)

func runList(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		stored, problems, err := store.Store{Root: root}.List()
		if err != nil {
			return fmt.Errorf("保持バージョンを読めませんでした: %w", err)
		}

		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"root":     root,
				"stored":   nonNil(stored),
				"problems": nonNil(problems),
			})
		}
		writeStored(cmd.OutOrStdout(), root, stored)
		for _, p := range problems {
			fmt.Fprintf(cmd.ErrOrStderr(), "警告: 保持バージョン %s を読めません: %s\n", p.ID, p.Reason)
		}
		return nil
	}
}

var variantLabels = map[string]string{"standard": "通常版", "portable": "portable"}

func writeStored(w io.Writer, root string, stored []store.Stored) {
	if len(stored) == 0 {
		fmt.Fprintln(w, "保持しているバージョンはありません。\"vmeltype install <tag>\" で取得します。")
		return
	}
	fmt.Fprintf(w, "保持しているバージョン (%s)\n\n", root)
	rows := [][]string{{"保持 ID", "タグ", "区分", "variant", "保持した日時"}}
	for _, s := range stored {
		kind := "正式版"
		if s.Prerelease {
			kind = "プレリリース"
		}
		variant := variantLabels[s.Variant]
		if variant == "" {
			variant = s.Variant
		}
		rows = append(rows, []string{s.ID, s.Tag, kind, variant, formatLocal(s.StoredAt)})
	}
	writeTable(w, rows)
}
