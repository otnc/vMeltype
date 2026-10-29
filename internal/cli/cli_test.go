package cli

import (
	"bytes"
	"strings"
	"testing"
)

type result struct {
	code   int
	stdout string
	stderr string
}

func execute(t *testing.T, args ...string) result {
	t.Helper()
	return executeIn(t, t.TempDir(), args...)
}

func executeIn(t *testing.T, root string, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, Env{In: strings.NewReader(""), Out: &stdout, Err: &stderr, Version: "1.2.3", DefaultRoot: func() (string, error) { return root, nil }})
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestRunWithoutArgumentsShowsHelp(t *testing.T) {
	got := execute(t)

	if got.code != ExitOK {
		t.Fatalf("code = %d, want %d (stderr: %s)", got.code, ExitOK, got.stderr)
	}
	for _, want := range []string{"使い方:", "コマンド:", "install", "use", "releases"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("ヘルプに %q がない:\n%s", want, got.stdout)
		}
	}
}

func TestAliasesBehaveLikeTheirCommands(t *testing.T) {
	aliases := map[string]string{
		"ls":  "list",
		"i":   "install",
		"un":  "uninstall",
		"u":   "use",
		"rel": "releases",
		"c":   "current",
		"up":  "update",
	}
	for alias, name := range aliases {
		t.Run(alias+" は "+name+" と同じ", func(t *testing.T) {
			viaAlias := execute(t, alias, "--help")
			viaName := execute(t, name, "--help")

			if viaAlias.code != ExitOK || viaAlias.stdout != viaName.stdout {
				t.Errorf("短縮名のヘルプが正式名と異なる\n短縮名 (%d):\n%s\n正式名 (%d):\n%s", viaAlias.code, viaAlias.stdout, viaName.code, viaName.stdout)
			}
		})
	}
}

func TestUsageErrorsExitWithTwo(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "未知のコマンド", args: []string{"frobnicate"}},
		{name: "前方一致では解決しない", args: []string{"lis"}},
		{name: "v はコマンドとして予約していて使えない", args: []string{"v"}},
		{name: "-v は --version の別名にしない", args: []string{"-v"}},
		{name: "未知のオプション", args: []string{"list", "--frobnicate"}},
		{name: "引数を取らないコマンドへの引数", args: []string{"current", "v1.4.0"}},
		{name: "必須の引数がない", args: []string{"uninstall"}},
		{name: "引数が多すぎる", args: []string{"install", "v1.4.0", "v1.5.0"}},
		{name: "そのコマンドにない短縮オプション", args: []string{"current", "-y"}},
		{name: "サブコマンドを持つコマンドの未知のサブコマンド", args: []string{"app", "frobnicate"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := execute(t, tt.args...)

			if got.code != ExitUsage {
				t.Errorf("code = %d, want %d (stdout: %s, stderr: %s)", got.code, ExitUsage, got.stdout, got.stderr)
			}
			if !strings.Contains(got.stderr, "--help") {
				t.Errorf("使い方の案内がない: %s", got.stderr)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"app", "version"}} {
		t.Run(strings.Join(args, " ")+" で vMeltype の版を表示する", func(t *testing.T) {
			got := execute(t, args...)

			if got.code != ExitOK || got.stdout != "vMeltype 1.2.3\n" {
				t.Errorf("code = %d, stdout = %q", got.code, got.stdout)
			}
		})
	}
}

func TestShortOptionsMatchLongOptions(t *testing.T) {
	tests := []struct {
		command []string
		short   string
		long    string
	}{
		{command: []string{"install", "v1.4.0", "--offline"}, short: "-y", long: "--yes"},
		{command: []string{"install", "v1.4.0", "--offline"}, short: "-n", long: "--dry-run"},
		{command: []string{"install", "v1.4.0"}, short: "-o", long: "--offline"},
		{command: []string{"use", "v1.4.0"}, short: "-f", long: "--force"},
		{command: []string{"apply"}, short: "-f", long: "--force"},
		{command: []string{"list"}, short: "-j", long: "--json"},
		{command: []string{"releases", "--cached"}, short: "-a", long: "--all"},
		{command: []string{"releases", "--cached"}, short: "-o", long: "--offline"},
		{command: []string{"uninstall", "v1.4.0"}, short: "-y", long: "--yes"},
		{command: []string{"app", "uninstall"}, short: "-n", long: "--dry-run"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.command, " ")+" の "+tt.short+" は "+tt.long+" と同じ", func(t *testing.T) {
			root := t.TempDir()
			viaShort := executeIn(t, root, append(tt.command, tt.short)...)
			viaLong := executeIn(t, root, append(tt.command, tt.long)...)

			if viaShort.code == ExitUsage || viaShort != viaLong {
				t.Errorf("短縮形と長い名前で結果が異なる\n短縮形: %+v\n長い名前: %+v", viaShort, viaLong)
			}
		})
	}
}

func TestUnimplementedCommandsFailWithoutChangingAnything(t *testing.T) {
	got := execute(t, "use", "v1.4.0", "-y")

	if got.code != ExitError {
		t.Errorf("code = %d, want %d", got.code, ExitError)
	}
	if !strings.Contains(got.stderr, "まだ実装していません") {
		t.Errorf("未実装であることを示していない: %s", got.stderr)
	}
}

func TestParentCommandsShowHelp(t *testing.T) {
	for _, parent := range []string{"app", "snapshot", "track"} {
		t.Run(parent+" だけならヘルプを表示する", func(t *testing.T) {
			got := execute(t, parent)

			if got.code != ExitOK || !strings.Contains(got.stdout, "コマンド:") {
				t.Errorf("code = %d, stdout = %s, stderr = %s", got.code, got.stdout, got.stderr)
			}
		})
	}
}
