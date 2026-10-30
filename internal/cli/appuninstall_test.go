package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otnc/vmeltype/internal/lifecycle"
)

// managedStandard は、本家のインストールを取り込んで v1.4.0 の通常版を有効にした状態を作る。本家の自動更新は止まっている。
func managedStandard(t *testing.T) *localEnv {
	t.Helper()
	l := newLocalEnv(t)
	l.storeStandard(t, "v1.4.0")
	l.installUpstream(t)
	if got := l.run(t, false, "", "use", "1.4.0", "-y"); got.code != ExitOK {
		t.Fatal(got.stderr)
	}
	return l
}

func (l *localEnv) autoUpdate(t *testing.T) any {
	t.Helper()
	var config map[string]any
	json.Unmarshal([]byte(readFile(filepath.Join(l.standardDataDir(), "config.json"))), &config)
	return config["AutoUpdate"]
}

func rootEntries(root string) []string {
	entries, _ := os.ReadDir(root)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestAppUninstallKeepsMeltype(t *testing.T) {
	t.Run("Meltype を残し、本家の自動更新を戻して、管理ルートを消す", func(t *testing.T) {
		l := managedStandard(t)

		got := l.run(t, false, "", "self", "uninstall", "-y")

		if got.code != ExitOK {
			t.Fatalf("code = %d, stderr = %s", got.code, got.stderr)
		}
		if _, err := os.Stat(l.root); !os.IsNotExist(err) {
			t.Errorf("管理ルートが残っている: %v", rootEntries(l.root))
		}
		if l.autoUpdate(t) != true {
			t.Errorf("本家の自動更新を戻していない: %v", l.autoUpdate(t))
		}
		if readFile(filepath.Join(l.standardAppDir(), "Meltype.exe")) != "exe v1.4.0" || len(l.removeCalls) != 0 {
			t.Errorf("Meltype を残していない")
		}
		if !strings.Contains(got.stdout, "vmeltype.exe") {
			t.Errorf("vmeltype.exe の消し方を示していない:\n%s", got.stdout)
		}
	})
	t.Run("--keep-snapshots ならスナップショットだけを残す", func(t *testing.T) {
		l := managedStandard(t)

		got := l.run(t, false, "", "app", "uninstall", "--keep-snapshots", "-y")

		if got.code != ExitOK || strings.Join(rootEntries(l.root), ",") != "snapshots" {
			t.Errorf("code = %d, 管理ルート = %v\n%s", got.code, rootEntries(l.root), got.stderr)
		}
	})
	t.Run("動いている Meltype を止めて、戻したあとに起動し直す", func(t *testing.T) {
		l := managedStandard(t)
		l.stopped, l.started = 0, 0
		l.running = []string{filepath.Join(l.standardAppDir(), "Meltype.exe")}

		got := l.run(t, false, "", "app", "uninstall", "-y")

		if got.code != ExitOK || l.stopped != 1 || l.started != 1 {
			t.Errorf("code = %d, stopped = %d, started = %d\n%s", got.code, l.stopped, l.started, got.stderr)
		}
	})
}

func TestAppUninstallQuestions(t *testing.T) {
	tests := []struct {
		name          string
		stdin         string
		wantCode      int
		wantRemove    bool
		wantSnapshots bool
		wantRoot      bool
	}{
		{name: "既定は Meltype を残し、スナップショットを消す", stdin: "\n\ny\n", wantCode: ExitOK},
		{name: "答えに従って Meltype を消し、スナップショットを残す", stdin: "y\nn\ny\n", wantCode: ExitOK, wantRemove: true, wantSnapshots: true},
		{name: "最後の確認の既定は中止", stdin: "\n\n\n", wantCode: ExitCanceled, wantRoot: true},
		{name: "途中で入力が閉じたら中止", stdin: "n\n", wantCode: ExitCanceled, wantRoot: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := managedStandard(t)

			got := l.run(t, true, tt.stdin, "app", "uninstall")

			if got.code != tt.wantCode {
				t.Fatalf("code = %d, want %d\n%s%s", got.code, tt.wantCode, got.stdout, got.stderr)
			}
			if (len(l.removeCalls) == 1) != tt.wantRemove {
				t.Errorf("Meltype の削除 = %v, want %v", l.removeCalls, tt.wantRemove)
			}
			_, err := os.Stat(filepath.Join(l.root, "snapshots"))
			if (err == nil) != (tt.wantSnapshots || tt.wantRoot) {
				t.Errorf("スナップショットの有無が違う: %v", rootEntries(l.root))
			}
			if _, err := os.Stat(filepath.Join(l.root, "versions")); (err == nil) != tt.wantRoot {
				t.Errorf("管理ルートの有無が違う: %v", rootEntries(l.root))
			}
		})
	}
}

func TestAppUninstallRemovesMeltype(t *testing.T) {
	t.Run("本家のアンインストーラーで消し、保持バージョンのうち有効なものの uninstall.ps1 を予備として渡す", func(t *testing.T) {
		l := managedStandard(t)

		got := l.run(t, false, "", "app", "uninstall", "--remove-meltype", "-y")

		if got.code != ExitOK || len(l.removeCalls) != 1 {
			t.Fatalf("code = %d, calls = %v\n%s", got.code, l.removeCalls, got.stderr)
		}
		call := l.removeCalls[0]
		if call.appDir != l.standardAppDir() || !strings.HasSuffix(call.fallback, filepath.Join("v1.4.0_windows-amd64_standard", "files", "uninstall.ps1")) || call.interactive {
			t.Errorf("渡したものが違う: %+v", call)
		}
		if _, err := os.Stat(l.root); !os.IsNotExist(err) {
			t.Errorf("管理ルートが残っている")
		}
	})
	t.Run("消せなければ、管理ルートを残す", func(t *testing.T) {
		l := managedStandard(t)
		l.removeFails = true

		got := l.run(t, false, "", "app", "uninstall", "--remove-meltype", "-y")

		if got.code != ExitError {
			t.Errorf("code = %d\n%s", got.code, got.stderr)
		}
		if _, err := os.Stat(filepath.Join(l.root, "baseline")); err != nil {
			t.Errorf("消せなかったのに管理ルートを消した: %v", rootEntries(l.root))
		}
	})
}

func TestAppUninstallStopsBeforeChanging(t *testing.T) {
	tests := []struct {
		name     string
		prepare  func(t *testing.T) *localEnv
		args     []string
		wantCode int
		wantText string
	}{
		{
			name:     "非対話で -y がなければ止める",
			prepare:  managedStandard,
			args:     []string{"app", "uninstall"},
			wantCode: ExitNeedsAction,
		},
		{
			name:     "--dry-run は計画だけを示す",
			prepare:  managedStandard,
			args:     []string{"app", "uninstall", "--dry-run"},
			wantCode: ExitOK,
			wantText: "スナップショット",
		},
		{
			name:     "中断した変更があれば、recover を案内する",
			prepare:  func(t *testing.T) *localEnv { return interruptedUse(t, lifecycle.PhaseOldMoved) },
			args:     []string{"app", "uninstall", "-y"},
			wantCode: ExitNeedsAction,
			wantText: "recover",
		},
		{
			name: "portable 版を残すなら、管理ルートの中にあるので止める",
			prepare: func(t *testing.T) *localEnv {
				l := newLocalEnv(t)
				l.storePortable(t, "v1.4.0", nil)
				l.run(t, false, "", "use", "1.4.0", "--portable", "-y")
				return l
			},
			args:     []string{"app", "uninstall", "-y"},
			wantCode: ExitError,
			wantText: "portable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := tt.prepare(t)
			before := rootEntries(l.root)

			got := l.run(t, false, "", tt.args...)

			if got.code != tt.wantCode || !strings.Contains(got.stdout+got.stderr, tt.wantText) {
				t.Errorf("code = %d, want %d\n%s%s", got.code, tt.wantCode, got.stdout, got.stderr)
			}
			if strings.Join(rootEntries(l.root), ",") != strings.Join(before, ",") || len(l.removeCalls) != 0 {
				t.Errorf("止めるべきなのに変えた: %v", rootEntries(l.root))
			}
		})
	}
}
