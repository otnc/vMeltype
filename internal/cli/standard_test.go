package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otnc/vmeltype/internal/snapshot"
	"github.com/otnc/vmeltype/internal/winime"
)

func (l *localEnv) standardAppDir() string {
	return filepath.Join(l.localAppData, "Programs", "Meltype")
}

func (l *localEnv) standardDataDir() string {
	return filepath.Join(l.localAppData, "Meltype")
}

// storeStandard は、本家の通常版の ZIP と同じく、app の下に本体があり、隣にインストール用のスクリプトがある保持版を置く。
func (l *localEnv) storeStandard(t *testing.T, tag string) {
	t.Helper()
	l.storeFiles(t, tag, "standard", map[string]string{
		"app/Meltype.exe":            "exe " + tag,
		"app/dotnet/host.dll":        "dll " + tag,
		"app/tip/x64/MeltypeTip.dll": "tip " + tag,
		"app/uninstall-helper.ps1":   "helper " + tag,
		"install.ps1":                "install " + tag,
		"uninstall.ps1":              "uninstall " + tag,
		"README.txt":                 "readme",
	})
}

// installUpstream は、本家の Install.cmd で入れたときと同じく、実行先とデータ先を置く。
func (l *localEnv) installUpstream(t *testing.T) {
	t.Helper()
	files := map[string]string{
		filepath.Join(l.standardAppDir(), "Meltype.exe"):          "exe 本家の 1.2.0",
		filepath.Join(l.standardAppDir(), "dotnet", "old.dll"):    "1.2.0 だけのファイル",
		filepath.Join(l.standardAppDir(), "uninstall.ps1"):        "本家が置いたアンインストール",
		filepath.Join(l.standardDataDir(), "config.json"):         "{\n  \"Enabled\": true,\n  \"AutoUpdate\": true\n}",
		filepath.Join(l.standardDataDir(), "userdict.txt"):        "辞書",
		filepath.Join(l.standardDataDir(), "update", "ready.txt"): "1.5.0\t...",
	}
	for path, body := range files {
		os.MkdirAll(filepath.Dir(path), 0o700)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "<なし>"
	}
	return string(data)
}

func TestUseStandard(t *testing.T) {
	t.Run("本家の app の中身を本家と同じ実行先に置き、自動更新を止める", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storeStandard(t, "v1.4.0")

		got := l.run(t, false, "", "use", "1.4.0", "-y")

		if got.code != ExitOK {
			t.Fatalf("code = %d, stderr = %s", got.code, got.stderr)
		}
		app := l.standardAppDir()
		if readFile(filepath.Join(app, "Meltype.exe")) != "exe v1.4.0" || readFile(filepath.Join(app, "tip", "x64", "MeltypeTip.dll")) != "tip v1.4.0" {
			t.Errorf("実行先に app の中身がない")
		}
		if readFile(filepath.Join(app, "install.ps1")) != "<なし>" {
			t.Errorf("app の外のファイルを実行先に置いた")
		}
		var config map[string]any
		json.Unmarshal([]byte(readFile(filepath.Join(l.standardDataDir(), "config.json"))), &config)
		if config["AutoUpdate"] != false {
			t.Errorf("本家の自動更新を止めていない: %v", config)
		}
		if s := l.status(t); s.Active == nil || s.Deployment != "match" || s.DataDir != l.standardDataDir() {
			t.Errorf("status が違う: %+v", s)
		}
	})

	t.Run("本家のインストールを取り込み、置き換えたファイルを残す", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storeStandard(t, "v1.4.0")
		l.installUpstream(t)

		got := l.run(t, false, "", "use", "1.4.0", "-y")

		if got.code != ExitOK {
			t.Fatalf("code = %d, stderr = %s", got.code, got.stderr)
		}
		app := l.standardAppDir()
		if readFile(filepath.Join(app, "Meltype.exe")) != "exe v1.4.0" || readFile(filepath.Join(app, "dotnet", "old.dll")) != "<なし>" {
			t.Errorf("本家のファイルを置き換えていない")
		}
		if readFile(filepath.Join(app, "uninstall.ps1")) != "本家が置いたアンインストール" {
			t.Errorf("本家のアンインストールを引き継いでいない")
		}
		if readFile(filepath.Join(l.root, "baseline", "standard-app", "Meltype.exe")) != "exe 本家の 1.2.0" {
			t.Errorf("置き換えた本家のファイルを管理前の記録に残していない")
		}
		data := l.standardDataDir()
		if config := readFile(filepath.Join(data, "config.json")); config != "{\n  \"Enabled\": true,\n  \"AutoUpdate\": false\n}" {
			t.Errorf("自動更新だけを止めていない: %q", config)
		}
		if readFile(filepath.Join(data, "update", "ready.txt")) != "<なし>" {
			t.Errorf("ダウンロード済みの更新をデータ先に残した")
		}
		if readFile(filepath.Join(data, "userdict.txt")) != "辞書" {
			t.Errorf("データを変えた")
		}
		listed, _, _ := snapshot.List(l.root)
		if len(listed) != 1 || listed[0].Reason != snapshot.ReasonBeforeUse || listed[0].Source != data {
			t.Fatalf("切り替えの前にデータを保存していない: %+v", listed)
		}
		for _, f := range listed[0].Files {
			if strings.HasPrefix(f.Path, "update/") {
				t.Errorf("ダウンロード済みの更新までスナップショットに入れた: %s", f.Path)
			}
		}
	})

	t.Run("次の切り替えでは、管理前の記録を書き換えない", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storeStandard(t, "v1.4.0")
		l.storeStandard(t, "v1.3.0")
		l.installUpstream(t)
		l.run(t, false, "", "use", "1.4.0", "-y")

		got := l.run(t, false, "", "use", "1.3.0", "-y")

		if got.code != ExitOK || readFile(filepath.Join(l.standardAppDir(), "Meltype.exe")) != "exe v1.3.0" {
			t.Fatalf("code = %d, stderr = %s", got.code, got.stderr)
		}
		if readFile(filepath.Join(l.standardAppDir(), "uninstall.ps1")) != "本家が置いたアンインストール" {
			t.Errorf("本家のアンインストールを引き継いでいない")
		}
		if readFile(filepath.Join(l.root, "baseline", "standard-app", "Meltype.exe")) != "exe 本家の 1.2.0" {
			t.Errorf("管理前の記録が変わった")
		}
		if s := l.status(t); s.Previous == nil || s.Previous.Tag != "v1.4.0" {
			t.Errorf("直前の版が違う: %+v", s)
		}
	})

	t.Run("動いている本家を終了させ、終わったら起動し直す", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storeStandard(t, "v1.4.0")
		l.installUpstream(t)
		l.running = []string{filepath.Join(l.standardAppDir(), "Meltype.exe")}

		got := l.run(t, false, "", "use", "1.4.0", "-y")

		if got.code != ExitOK || l.stopped != 1 || l.started != 1 {
			t.Errorf("code = %d, stopped = %d, started = %d, stderr = %s", got.code, l.stopped, l.started, got.stderr)
		}
	})
}

func TestUseStandardReplacesRegisteredIME(t *testing.T) {
	t.Run("登録済みなら、実行先を入れ替える前に、切り替え先の版の DLL に入れ替える", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storeStandard(t, "v1.4.0")
		l.installUpstream(t)
		l.imeRegistered = true
		l.imeResult = winime.ResultReplaced

		got := l.run(t, false, "", "use", "1.4.0", "-y")

		if got.code != ExitOK || readFile(filepath.Join(l.standardAppDir(), "Meltype.exe")) != "exe v1.4.0" {
			t.Fatalf("code = %d, stderr = %s", got.code, got.stderr)
		}
		if len(l.imeCalls) != 1 || readFile(filepath.Join(l.imeCalls[0].appDir, "tip", "x64", "MeltypeTip.dll")) != "tip v1.4.0" {
			t.Errorf("切り替え先の版の DLL を渡していない: %+v", l.imeCalls)
		}
		if l.imeCalls[0].deployed != "exe 本家の 1.2.0" {
			t.Errorf("実行先を入れ替えてから DLL を入れ替えた")
		}
	})
	t.Run("登録していなければ、DLL に触れない", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storeStandard(t, "v1.4.0")

		l.run(t, false, "", "use", "1.4.0", "-y")

		if len(l.imeCalls) != 0 {
			t.Errorf("登録していないのに DLL を入れ替えた")
		}
	})
	t.Run("--dry-run は入れ替えることを示すだけ", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storeStandard(t, "v1.4.0")
		l.imeRegistered = true

		got := l.run(t, false, "", "use", "1.4.0", "--dry-run")

		if got.code != ExitOK || !strings.Contains(got.stdout, "Meltype IME") || len(l.imeCalls) != 0 {
			t.Errorf("code = %d, calls = %d\n%s", got.code, len(l.imeCalls), got.stdout)
		}
	})
}

func TestUseStandardStopsWhenIMECannotBeReplaced(t *testing.T) {
	tests := []struct {
		result   winime.Result
		wantCode int
		wantText string
	}{
		{result: winime.ResultCanceled, wantCode: ExitNeedsAction, wantText: "管理者権限"},
		{result: winime.ResultFailed, wantCode: ExitNeedsAction, wantText: "登録できませんでした"},
		{result: winime.ResultUnsupported, wantCode: ExitError, wantText: "対応していません"},
		{result: winime.ResultCorrupt, wantCode: ExitError, wantText: "--reinstall"},
	}
	for _, tt := range tests {
		t.Run(string(tt.result)+" なら実行先を変えずに止める", func(t *testing.T) {
			l := newLocalEnv(t)
			l.storeStandard(t, "v1.4.0")
			l.installUpstream(t)
			l.imeRegistered = true
			l.imeResult = tt.result
			l.running = []string{filepath.Join(l.standardAppDir(), "Meltype.exe")}

			got := l.run(t, false, "", "use", "1.4.0", "-y")

			if got.code != tt.wantCode || !strings.Contains(got.stderr, tt.wantText) {
				t.Errorf("code = %d, want %d\n%s", got.code, tt.wantCode, got.stderr)
			}
			if readFile(filepath.Join(l.standardAppDir(), "Meltype.exe")) != "exe 本家の 1.2.0" {
				t.Errorf("止めるべきなのに実行先を変えた")
			}
			if s := l.status(t); s.Active != nil {
				t.Errorf("止めるべきなのに有効版を記録した: %+v", s)
			}
			if l.started != 1 {
				t.Errorf("終了させた本家を起動し直していない")
			}
		})
	}
}

func TestSnapshotUsesStandardData(t *testing.T) {
	l := newLocalEnv(t)
	l.storeStandard(t, "v1.4.0")
	l.installUpstream(t)
	l.run(t, false, "", "use", "1.4.0", "-y")

	got := l.run(t, false, "", "snapshot", "create", "-y", "--json")

	var m snapshot.Manifest
	json.Unmarshal([]byte(got.stdout), &m)
	if got.code != ExitOK || m.Source != l.standardDataDir() {
		t.Errorf("code = %d, source = %q, stderr = %s", got.code, m.Source, got.stderr)
	}
}
