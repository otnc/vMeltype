// Package cli は、vmeltype のコマンドと引数を解釈し、終了コードを決める。
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/otnc/vmeltype/internal/assets"
	"github.com/otnc/vmeltype/internal/winime"
)

// Env は、コマンドが使う入出力と、外の世界への依存。テストで差し替えられるよう、ここからだけ受け取る。
// Interactive は、標準入力と表示先の両方が TTY のときだけ true にする。
type Env struct {
	In          io.Reader
	Out         io.Writer
	Err         io.Writer
	Interactive bool
	Version     string
	Now         func() time.Time
	Getenv      func(string) string
	// DefaultRoot は、--root がないときの管理ルートを返す。
	DefaultRoot func() (string, error)
	// HTTPClient と GitHubAPIURL は、公式一覧の取得など、通信するコマンドだけが使う。
	HTTPClient   *http.Client
	GitHubAPIURL string
	// Platform は、配布物を選ぶときのこの端末の OS と CPU。
	Platform assets.Platform
	// RunningUnder は、ディレクトリの下の実行ファイルから動いているプロセスを返す。
	RunningUnder func(dir string) ([]string, error)
	// StopApp は、実行先の Meltype に終了を求め、終わるまで待つ。終わらなくても強制終了しない。
	StopApp func(appDir string) error
	// StartApp は、実行先の Meltype を起動し、終わるのを待たない。
	StartApp func(appDir string) error
	// LocalAppData は、通常版の実行先とデータ先を決める、このユーザーの LocalAppData を返す。
	LocalAppData func() (string, error)
	// IMERegistered は、Meltype IME が PC に登録されているかを返す。
	IMERegistered func() (bool, error)
	// ReplaceIME は、登録済みの Meltype IME の DLL を、appDir (切り替え先の版の、本家の ZIP の app に当たるディレクトリ) のものに入れ替える。
	ReplaceIME func(appDir string) (winime.Result, error)
}

const usageTemplate = `使い方:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} <コマンド>{{end}}{{if gt (len .Aliases) 0}}

短縮名:
  {{.NameAndAliases}}{{end}}{{if .HasAvailableSubCommands}}

コマンド:{{range .Commands}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

オプション:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

共通のオプション:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

"{{.CommandPath}} <コマンド> --help" で、各コマンドの説明を表示します。{{end}}
`

// Run は、引数を解釈してコマンドを実行し、終了コードを返す。
//
//	os.Exit(cli.Run(os.Args[1:], env))
func Run(args []string, env Env) int {
	return RunContext(context.Background(), args, env)
}

// RunContext は Run と同じ。ctx が中止されたら (Ctrl+C など)、中止の終了コードを返す。
func RunContext(ctx context.Context, args []string, env Env) int {
	root := newRootCommand(&env)
	root.SetArgs(args)
	root.SetIn(env.In)
	root.SetOut(env.Out)
	root.SetErr(env.Err)

	executed, err := root.ExecuteContextC(ctx)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	code := exitCodeOf(err)
	if err != nil {
		fmt.Fprintf(env.Err, "エラー: %v\n", err)
		if code == ExitUsage {
			fmt.Fprintf(env.Err, "\"%s --help\" で使い方を表示します。\n", executed.CommandPath())
		}
	}
	return code
}

func newRootCommand(env *Env) *cobra.Command {
	root := &cobra.Command{
		Use:   "vmeltype",
		Short: "Meltype の複数の版を保持し、切り替える",
		Long: `vMeltype は、Meltype の正式版、プレリリース、旧版を保持し、選んで切り替える版の管理ツールです。
install で取得して保持し、use で有効にします。use、list、current などのローカル操作は通信しません。`,
		Version:               env.Version,
		Args:                  rejectUnknownSubcommand,
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,
		SilenceUsage:          true,
		// 引数なしでは、何も変更せずにヘルプを表示する
		RunE:              func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	// v は版の指定と紛らわしいので、--version の短縮形にしない
	root.Flags().Bool("version", false, "vMeltype の版を表示する")
	root.PersistentFlags().String("root", "", "vMeltype の管理ルート (設定、保持版、スナップショットを置く場所)")
	root.SetVersionTemplate("vMeltype {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{fmt.Errorf("オプションの誤り: %w", err)}
	})

	for _, spec := range commandSpecs(env) {
		root.AddCommand(buildCommand(spec))
	}

	root.InitDefaultHelpCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "help" {
			cmd.Short = "コマンドの説明を表示する"
			cmd.Hidden = true
		}
	}
	applyJapaneseHelp(root)
	return root
}

func applyJapaneseHelp(cmd *cobra.Command) {
	cmd.SetUsageTemplate(usageTemplate)
	cmd.InitDefaultHelpFlag()
	cmd.Flags().Lookup("help").Usage = "ヘルプを表示する"
	for _, sub := range cmd.Commands() {
		applyJapaneseHelp(sub)
	}
}

// managementRoot は、--root か既定の管理ルートを、絶対パスで返す。
func managementRoot(cmd *cobra.Command, env *Env) (string, error) {
	root, err := cmd.Flags().GetString("root")
	if err != nil {
		return "", err
	}
	if root == "" {
		return env.DefaultRoot()
	}
	return filepath.Abs(root)
}
