package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otnc/vmeltype/internal/config"
	"github.com/otnc/vmeltype/internal/lifecycle"
)

func addStopAppFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("stop-app", false, "実行先の Meltype が動いていれば、確認せずに正常に終了させてから進める。終わったら起動し直す")
}

// appStop は、変更の前に、実行先で動いている Meltype を終了させるかどうか。
type appStop struct {
	appDir  string
	running []string
	// consented は、終了させてよいと決まっていること (--stop-app か、設定で許した -y)。
	consented bool
}

// inspectApp は、実行先で Meltype が動いているかと、確認なしで終了させてよいかを調べる。
func inspectApp(cmd *cobra.Command, env *Env, root, appDir string) (appStop, error) {
	running, err := env.RunningUnder(appDir)
	if err != nil {
		return appStop{}, fmt.Errorf("実行先の Meltype が動いているかを確かめられませんでした: %w", err)
	}
	s := appStop{appDir: appDir, running: running}
	if !s.needed() {
		return s, nil
	}
	flags := cmd.Flags()
	stopApp, _ := flags.GetBool("stop-app")
	yes, _ := flags.GetBool("yes")
	if stopApp {
		s.consented = true
	} else if yes {
		settings, err := config.Load(root)
		if err != nil {
			return appStop{}, err
		}
		s.consented = settings.StopAppOnYes
	}
	return s, nil
}

func (s appStop) needed() bool {
	return len(s.running) > 0
}

// describe は、計画の表示に、Meltype を終了させることを足す。
func (s appStop) describe(w io.Writer) {
	if s.needed() {
		fmt.Fprintln(w, "実行先の Meltype が動いているので、正常に終了させてから進め、終わったら起動し直します。")
	}
}

// confirmable は、確認できない環境で、終了させてよいかを決められなければ止める。
func (s appStop) confirmable(interactive bool) error {
	if s.needed() && !s.consented && !interactive {
		return fmt.Errorf("%w (%s)。終了させてから進めるには --stop-app を付けてください", errAppRunning, strings.Join(s.running, "、"))
	}
	return nil
}

// assumeYes は、-y で確認を省いてよいか。終了させてよいと決まっていなければ、-y でも聞く。
func (s appStop) assumeYes(yes bool) bool {
	return yes && (!s.needed() || s.consented)
}

// question は、終了させてよいかを、変更の確認と一緒に聞く文言にする。
func (s appStop) question(q string) string {
	if s.needed() && !s.consented {
		return "Meltype を終了して、" + q
	}
	return q
}

// stop は、確認のあとに Meltype を終了させる。終了させたかと、終わったあとに動いていないことを返す。
// 確認を待つ間に起動されることがあるので、終了させなかったときも動いていないことを確かめる。
func (s appStop) stop(env *Env) (bool, error) {
	if s.needed() {
		if err := env.StopApp(s.appDir); err != nil {
			return false, err
		}
	}
	running, err := env.RunningUnder(s.appDir)
	if err != nil {
		return s.needed(), fmt.Errorf("実行先の Meltype が動いているかを確かめられませんでした: %w", err)
	}
	if len(running) > 0 {
		return s.needed(), fmt.Errorf("%w (%s)", errAppRunning, strings.Join(running, "、"))
	}
	return s.needed(), nil
}

// restartApp は、終了させた Meltype を、変更のあとに起動し直す。recover が要る失敗では、実行先が定まっていないので起動しない。
// 起動できなくても変更は終わっているので、警告だけにする。
func restartApp(cmd *cobra.Command, env *Env, appDir string, err error) {
	w := cmd.ErrOrStderr()
	if errors.Is(err, lifecycle.ErrNeedsRecover) {
		fmt.Fprintln(w, "Meltype は起動し直していません。復旧してから起動してください。")
		return
	}
	if startErr := env.StartApp(appDir); startErr != nil {
		env.warnf("%v", startErr)
		return
	}
	fmt.Fprintln(w, "Meltype を起動し直しました。")
}
