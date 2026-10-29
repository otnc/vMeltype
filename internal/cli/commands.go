package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// commonFlag は、複数のコマンドで同じ意味を持つオプション。短縮形はここでだけ決める。
type commonFlag int

const (
	flagYes commonFlag = iota
	flagForce
	flagJSON
	flagDryRun
	flagAll
	flagOffline
)

var commonFlagDefinitions = map[commonFlag]struct {
	name, shorthand, usage string
}{
	flagYes:     {"yes", "y", "計画が決まったあとの通常の確認を省く"},
	flagForce:   {"force", "f", "同じ版でも省かずに、安全な再適用を行う"},
	flagJSON:    {"json", "j", "結果を JSON で標準出力に出す"},
	flagDryRun:  {"dry-run", "n", "計画だけを表示し、変更しない"},
	flagAll:     {"all", "a", "プレリリースも含める"},
	flagOffline: {"offline", "o", "取得を禁止し、保存済みの情報だけを使う"},
}

// change は、変更を伴うコマンドに共通のオプション。
var change = []commonFlag{flagYes, flagDryRun, flagJSON}

type commandSpec struct {
	use     string
	aliases []string
	short   string
	long    string
	args    cobra.PositionalArgs
	flags   []commonFlag
	run     func(cmd *cobra.Command, args []string) error
	subs    []commandSpec
	// setup は、そのコマンドだけのオプションを足す。
	setup func(cmd *cobra.Command)
}

func commandSpecs(env *Env) []commandSpec {
	return []commandSpec{
		{use: "current", aliases: []string{"c"}, short: "有効な Meltype の版を表示する", flags: []commonFlag{flagJSON}},
		{use: "list", aliases: []string{"ls"}, short: "保持版を表示する。通信しない", flags: []commonFlag{flagJSON}, run: runList(env)},
		{use: "status", short: "要求、配置、登録、稼働、データ先、未完了の処理を表示する", flags: []commonFlag{flagJSON}},
		{use: "fetch", short: "公式のリリースの情報だけを取得して保存する。配布物は取得しない", flags: []commonFlag{flagJSON}, run: runFetch(env)},
		{use: "releases", aliases: []string{"rel"}, short: "公式のリリースの一覧を取得して表示する", long: releasesLong, flags: []commonFlag{flagAll, flagOffline, flagJSON}, setup: setupReleasesFlags, run: runReleases(env)},
		{use: "install [tag]", aliases: []string{"i"}, short: "版を取得、検証して保持する。有効な版は変えない", long: installLong, args: maxArgs(1), flags: append([]commonFlag{flagOffline}, change...), setup: setupInstallFlags, run: runInstall(env)},
		{use: "use [tag]", aliases: []string{"u"}, short: "保持版を有効にする。通信しない", args: maxArgs(1), flags: append([]commonFlag{flagForce}, change...)},
		{use: "apply", short: "保存済みの対象を適用、修復する。通信しない", flags: append([]commonFlag{flagForce}, change...)},
		{use: "uninstall <tag>", aliases: []string{"un"}, short: "有効でない保持版を削除する", args: exactArgs(1), flags: change},
		{use: "check", short: "新しい版があるかを確かめる。適用しない", flags: []commonFlag{flagJSON}},
		{use: "update", aliases: []string{"up"}, short: "新しい版を取得して有効にする", flags: change},
		{use: "pin [tag]", short: "有効な版を固定する", args: maxArgs(1), flags: []commonFlag{flagJSON}},
		{use: "unpin", short: "版の固定を解除する", flags: []commonFlag{flagJSON}},
		{use: "rollback", short: "直前に有効にした版へ戻す。データは戻さない", flags: change},
		{use: "recover", short: "中断した変更を処理記録から復旧する", flags: change},
		{use: "snapshot", short: "データの保存と復元", subs: []commandSpec{
			{use: "create", short: "本家を止めてデータを保存する", flags: change},
			{use: "list", short: "保存したスナップショットを表示する", flags: []commonFlag{flagJSON}},
			{use: "inspect <id>", short: "スナップショットの範囲を表示する", args: exactArgs(1), flags: []commonFlag{flagJSON}},
			{use: "verify <id>", short: "スナップショットの完全性を検査する", args: exactArgs(1), flags: []commonFlag{flagJSON}},
			{use: "restore <id>", short: "いまのデータを退避してからスナップショットを復元する", args: exactArgs(1), flags: change},
		}},
		{use: "doctor", short: "依存、権限、登録、データ先、復旧できるかを診断する", flags: []commonFlag{flagJSON}},
		{use: "run", short: "有効な版の Meltype を起動する"},
		{use: "detach", short: "いまの環境を残して管理をやめる", flags: change},
		{use: "track", short: "新しい版への自動の追従", subs: []commandSpec{
			{use: "status", short: "追従の設定を表示する", flags: []commonFlag{flagJSON}},
			{use: "enable", short: "追従を有効にする", flags: []commonFlag{flagJSON}},
			{use: "disable", short: "追従を無効にする", flags: []commonFlag{flagJSON}},
		}},
		{use: "app", short: "vMeltype 自身の操作", subs: []commandSpec{
			{use: "version", short: "vMeltype の版を表示する", run: printVersion(env.Version)},
			{use: "doctor", short: "vMeltype 自身と管理ルートを診断する", flags: []commonFlag{flagJSON}},
			{use: "update", short: "vMeltype 自身を更新する", flags: change},
			{use: "uninstall", short: "Meltype を管理前に戻してから vMeltype を削除する", flags: change},
			{use: "config", short: "vMeltype の設定", subs: []commandSpec{
				{use: "show", short: "設定を表示する", flags: []commonFlag{flagJSON}},
				{use: "set <key> <value>", short: "設定を変える。既存のファイルは移動しない", args: exactArgs(2), flags: []commonFlag{flagJSON}},
			}},
		}},
	}
}

func buildCommand(spec commandSpec) *cobra.Command {
	cmd := &cobra.Command{
		Use:                   spec.use,
		Aliases:               spec.aliases,
		Short:                 spec.short,
		Long:                  spec.long,
		Args:                  spec.args,
		RunE:                  spec.run,
		DisableFlagsInUseLine: true,
	}
	for _, flag := range spec.flags {
		def := commonFlagDefinitions[flag]
		cmd.Flags().BoolP(def.name, def.shorthand, false, def.usage)
	}
	if spec.setup != nil {
		spec.setup(cmd)
	}

	if len(spec.subs) > 0 {
		cmd.Args = rejectUnknownSubcommand
		cmd.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
		for _, sub := range spec.subs {
			cmd.AddCommand(buildCommand(sub))
		}
		return cmd
	}
	if cmd.Args == nil {
		cmd.Args = noArgs
	}
	if cmd.RunE == nil {
		cmd.RunE = notImplemented
	}
	return cmd
}

func notImplemented(cmd *cobra.Command, _ []string) error {
	return fmt.Errorf("%s はまだ実装していません", cmd.CommandPath())
}

func printVersion(version string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "vMeltype %s\n", version)
		return err
	}
}

func rejectUnknownSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError{fmt.Errorf("不明なコマンドです: %q", args[0])}
	}
	return nil
}

func noArgs(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError{fmt.Errorf("引数は受け付けません: %q", args[0])}
	}
	return nil
}

func maxArgs(n int) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) > n {
			return usageError{fmt.Errorf("引数は %d 個までです (%d 個あります)", n, len(args))}
		}
		return nil
	}
}

func exactArgs(n int) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != n {
			return usageError{fmt.Errorf("引数は %d 個必要です (%d 個あります)", n, len(args))}
		}
		return nil
	}
}
