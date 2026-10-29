// Package cli は、vmeltype のコマンドと引数を解釈し、終了コードを決める。
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Streams は、コマンドが使う入出力。Interactive は、標準入力と表示先の両方が TTY のときだけ true にする。
type Streams struct {
	In          io.Reader
	Out         io.Writer
	Err         io.Writer
	Interactive bool
}

const usageTemplate = `使い方:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} <コマンド>{{end}}{{if gt (len .Aliases) 0}}

短縮名:
  {{.NameAndAliases}}{{end}}{{if .HasAvailableSubCommands}}

コマンド:{{range .Commands}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

オプション:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

"{{.CommandPath}} <コマンド> --help" で、各コマンドの説明を表示します。{{end}}
`

// Run は、引数を解釈してコマンドを実行し、終了コードを返す。
//
//	os.Exit(cli.Run(os.Args[1:], cli.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}, version))
func Run(args []string, streams Streams, version string) int {
	return RunContext(context.Background(), args, streams, version)
}

// RunContext は Run と同じ。ctx が中止されたら (Ctrl+C など)、中止の終了コードを返す。
func RunContext(ctx context.Context, args []string, streams Streams, version string) int {
	root := newRootCommand(version)
	root.SetArgs(args)
	root.SetIn(streams.In)
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)

	executed, err := root.ExecuteContextC(ctx)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	code := exitCodeOf(err)
	if err != nil {
		fmt.Fprintf(streams.Err, "エラー: %v\n", err)
		if code == ExitUsage {
			fmt.Fprintf(streams.Err, "\"%s --help\" で使い方を表示します。\n", executed.CommandPath())
		}
	}
	return code
}

func newRootCommand(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "vmeltype",
		Short: "Meltype の複数の版を保持し、切り替える",
		Long: `vMeltype は、Meltype の正式版、プレリリース、旧版を保持し、選んで切り替える版の管理ツールです。
install で取得して保持し、use で有効にします。use、list、current などのローカル操作は通信しません。`,
		Version:               version,
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
	root.SetVersionTemplate("vMeltype {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{fmt.Errorf("オプションの誤り: %w", err)}
	})

	for _, spec := range commandSpecs(version) {
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
