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
	flagForce:   {"force", "f", "同じバージョンでも省かずに、安全な再適用を行う"},
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
		{use: "current", aliases: []string{"c"}, short: "有効な Meltype のバージョンを表示する。通信しない", flags: []commonFlag{flagJSON}, run: runCurrent(env)},
		{use: "list", aliases: []string{"ls"}, short: "保持バージョンを表示する。通信しない", flags: []commonFlag{flagJSON}, run: runList(env)},
		{use: "status", short: "有効なバージョン、配置、稼働、データ先、未完了の処理を表示する。通信しない", flags: []commonFlag{flagJSON}, run: runStatus(env)},
		{use: "fetch", short: "公式のリリースの情報だけを取得して保存する。配布物は取得しない", flags: []commonFlag{flagJSON}, run: runFetch(env)},
		{use: "releases", aliases: []string{"rel"}, short: "公式のリリースの一覧を取得して表示する", long: releasesLong, flags: []commonFlag{flagAll, flagOffline, flagJSON}, setup: setupReleasesFlags, run: runReleases(env)},
		{use: "install [tag]", aliases: []string{"i"}, short: "バージョンを取得、検証して保持する。有効なバージョンは変えない", long: installLong, args: maxArgs(1), flags: append([]commonFlag{flagOffline}, change...), setup: setupInstallFlags, run: runInstall(env)},
		{use: "use [tag]", aliases: []string{"u"}, short: "保持バージョンを有効にする。通信しない", long: useLong, args: maxArgs(1), flags: append([]commonFlag{flagForce}, change...), setup: setupUseFlags, run: runUse(env)},
		{use: "apply", short: "保存済みの対象を適用、修復する。通信しない", flags: append([]commonFlag{flagForce}, change...)},
		{use: "uninstall <tag>", aliases: []string{"un"}, short: "有効でない保持バージョンを削除する。バージョンは省略せずに指定する。実行先と data には触れない", args: exactArgs(1), flags: change, setup: setupUninstallFlags, run: runUninstall(env)},
		{use: "check", short: "新しいバージョンがあるかを確かめる。適用しない", flags: []commonFlag{flagJSON}, setup: setupCheckFlags, run: runCheck(env)},
		{use: "update", aliases: []string{"up", "upgrade"}, short: "新しいバージョンを取得して有効にする", long: updateLong, flags: change, setup: setupUpdateFlags, run: runUpdate(env)},
		{use: "pin [tag]", short: "有効なバージョンを固定する。通信しない", long: pinLong, args: maxArgs(1), flags: []commonFlag{flagJSON}, run: runPin(env)},
		{use: "unpin", short: "バージョンの固定を解除する。有効なバージョンは変えない", flags: []commonFlag{flagJSON}, run: runUnpin(env)},
		{use: "rollback", short: "直前に有効にしたバージョンへ戻す。データは戻さない", flags: change, setup: setupRollbackFlags, run: runRollback(env)},
		{use: "recover", short: "中断した変更を処理記録から復旧する", long: recoverLong, flags: change, run: runRecover(env)},
		{use: "snapshot", short: "データの保存と復元。通信しない", long: snapshotLong, subs: []commandSpec{
			{use: "create", short: "Meltype が止まっていることを確かめてデータを保存する。動いていれば終了させてから保存する", flags: change, setup: addStopAppFlag, run: runSnapshotCreate(env)},
			{use: "list", short: "保存したスナップショットを表示する", flags: []commonFlag{flagJSON}, run: runSnapshotList(env)},
			{use: "inspect <id>", short: "スナップショットの範囲を表示する", args: exactArgs(1), flags: []commonFlag{flagJSON}, run: runSnapshotInspect(env)},
			{use: "verify <id>", short: "スナップショットの中身を記録と照合する", args: exactArgs(1), flags: []commonFlag{flagJSON}, run: runSnapshotVerify(env)},
			{use: "restore <id>", short: "いまのデータを保存してからスナップショットを復元する", args: exactArgs(1), flags: change, setup: addStopAppFlag, run: runSnapshotRestore(env)},
			{use: "export <id> <file>", short: "スナップショットを 1 つのファイルに書き出す。別の PC で取り込める", args: exactArgs(2), flags: []commonFlag{flagJSON}, run: runSnapshotExport(env)},
			{use: "import <file>", short: "書き出したスナップショットを取り込む。データは変えない", args: exactArgs(1), flags: []commonFlag{flagJSON}, run: runSnapshotImport(env)},
			{use: "remove <id>", short: "スナップショットを削除する。データは変えない", args: exactArgs(1), flags: change, run: runSnapshotRemove(env)},
			{use: "prune", short: "自動で作ったスナップショットの古いものを削除する。手動で作ったものと取り込んだものは残す", flags: change, setup: func(cmd *cobra.Command) {
				cmd.Flags().Int("keep", 0, "自動で作ったものを、新しいほうからいくつ残すか (既定は設定 auto-snapshot-keep の値)")
			}, run: runSnapshotPrune(env)},
		}},
		{use: "doctor", short: "管理ルートを診断し、直し方を示す。通信しない", flags: []commonFlag{flagJSON}, run: runDoctor(env)},
		{use: "run", short: "有効なバージョンの Meltype を起動する。通信しない", run: runRun(env)},
		{use: "detach", short: "いまの環境を残して管理をやめる", flags: change},
		{use: "track", short: "新しいバージョンへの自動の追従", subs: []commandSpec{
			{use: "status", short: "追従の設定を表示する", flags: []commonFlag{flagJSON}},
			{use: "enable", short: "追従を有効にする", flags: []commonFlag{flagJSON}},
			{use: "disable", short: "追従を無効にする", flags: []commonFlag{flagJSON}},
		}},
		{use: "app", aliases: []string{"self"}, short: "vMeltype 自身の操作", subs: []commandSpec{
			{use: "version", short: "vMeltype のバージョンを表示する", run: printVersion(env.Version)},
			{use: "doctor", short: "vMeltype 自身と管理ルートを診断する", flags: []commonFlag{flagJSON}},
			{use: "update", aliases: []string{"upgrade"}, short: "vMeltype 自身を新しいバージョンに置き換える", long: appUpdateLong, flags: change, setup: setupAppUpdateFlags, run: runAppUpdate(env)},
			{use: "uninstall", short: "vMeltype をやめる。Meltype を残すか消すかを選べる", long: appUninstallLong, flags: change, setup: setupAppUninstallFlags, run: runAppUninstall(env)},
			{use: "config", short: "vMeltype の設定", subs: []commandSpec{
				{use: "show", short: "設定を表示する", flags: []commonFlag{flagJSON}, run: runConfigShow(env)},
				{use: "set <key> <value>", short: "設定を変える。既存のファイルは移動しない", args: exactArgs(2), flags: []commonFlag{flagJSON}, run: runConfigSet(env)},
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
		// 使えるサブコマンドが 1 つもなければ、親も一覧から隠す
		cmd.Hidden = true
		for _, sub := range spec.subs {
			child := buildCommand(sub)
			cmd.AddCommand(child)
			cmd.Hidden = cmd.Hidden && child.Hidden
		}
		return cmd
	}
	if cmd.Args == nil {
		cmd.Args = noArgs
	}
	if cmd.RunE == nil {
		// まだ作っていないコマンドは一覧に出さず、打たれたら、まだないことを示す
		cmd.RunE = notImplemented
		cmd.Hidden = true
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
