package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otnc/meltool/internal/appctl"
	"github.com/otnc/meltool/internal/lifecycle"
)

// runRun は、有効なバージョンの Meltype を起動して、終わるのを待たずに戻る。Meltype には引数を渡さない。
// 起動のたびに全ファイルの中身を照合すると遅いので、実行先の配置は doctor と status で確かめる。
func runRun(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		// 中断した変更があると、実行先がどのバージョンか定まっていない
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
			return errors.New("有効なバージョンがないので起動できません。先に \"meltool use <tag>\" で有効にしてください")
		}
		appDir := state.Active.AppDir
		if _, err := os.Stat(filepath.Join(appDir, appctl.ExeName)); err != nil {
			return fmt.Errorf("実行先に %s がありません。\"%s\" で置き直してください: %w", appctl.ExeName, reapplyCommand(state.Active), err)
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
