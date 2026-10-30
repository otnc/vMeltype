package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otnc/vmeltype/internal/appctl"
	"github.com/otnc/vmeltype/internal/lifecycle"
)

// runRun は、有効版の Meltype を起動して、終わるのを待たずに戻る。Meltype には引数を渡さない。
// 起動のたびに全ファイルの中身を照合すると遅いので、実行先の配置は doctor と status で確かめる。
func runRun(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		// 中断した変更があると、実行先がどの版か定まっていない
		if journal, err := lifecycle.LoadJournal(root); err != nil {
			return err
		} else if journal != nil {
			return lifecycle.ErrPendingJournal
		}
		state, err := lifecycle.LoadState(root)
		if err != nil {
			return err
		}
		if state.Active == nil {
			return errors.New("有効な版がないので起動できません。先に \"vmeltype use <tag>\" で有効にしてください")
		}
		appDir := state.Active.AppDir
		if _, err := os.Stat(filepath.Join(appDir, appctl.ExeName)); err != nil {
			return fmt.Errorf("実行先に %s がありません。\"vmeltype use %s --portable --force\" で置き直してください: %w", appctl.ExeName, state.Active.Tag, err)
		}

		out := cmd.OutOrStdout()
		running, err := env.RunningUnder(appDir)
		if err != nil {
			return fmt.Errorf("実行先の Meltype が動いているかを確かめられませんでした: %w", err)
		}
		if len(running) > 0 {
			fmt.Fprintf(out, "%s の Meltype はもう動いています (%s)。\n", state.Active.Tag, strings.Join(running, "、"))
			return nil
		}
		if err := env.StartApp(appDir); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s の Meltype を起動しました。\n", state.Active.Tag)
		return nil
	}
}
