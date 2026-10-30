package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// runningEnv は、v1.4.0 が有効で v1.3.0 も保持し、実行先の Meltype が動いている状態を作る。
func runningEnv(t *testing.T) *localEnv {
	t.Helper()
	l := newLocalEnv(t)
	l.storePortable(t, "v1.4.0", nil)
	l.storePortable(t, "v1.3.0", nil)
	l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")
	l.writeData(t, "userdict.txt", "辞書")
	l.running = []string{filepath.Join(l.appDir(), "Meltype.exe")}
	return l
}

func TestStopAppBeforeSwitching(t *testing.T) {
	tests := []struct {
		name         string
		stopAppOnYes string
		interactive  bool
		stdin        string
		args         []string
		wantCode     int
		wantStopped  bool
		wantText     string
	}{
		{name: "-y だけで終了させ、終わったら起動し直す", args: []string{"-y"}, wantCode: ExitOK, wantStopped: true},
		{name: "設定で許していなければ、-y だけでは終了させない", stopAppOnYes: "false", args: []string{"-y"}, wantCode: ExitNeedsAction, wantText: "--stop-app"},
		{name: "--stop-app は設定に関わらず終了させる", stopAppOnYes: "false", args: []string{"--stop-app", "-y"}, wantCode: ExitOK, wantStopped: true},
		{name: "--stop-app を付ければ、終了してよいかは聞かない", stopAppOnYes: "false", interactive: true, stdin: "y\n", args: []string{"--stop-app"}, wantCode: ExitOK, wantStopped: true},
		{name: "対話では終了してよいかを一緒に聞く", interactive: true, stdin: "y\n", args: nil, wantCode: ExitOK, wantStopped: true, wantText: "Meltype を終了して"},
		{name: "対話で断れば終了させない", interactive: true, stdin: "n\n", args: nil, wantCode: ExitCanceled},
		{name: "設定で許していなければ、対話では -y でも聞く", stopAppOnYes: "false", interactive: true, stdin: "y\n", args: []string{"-y"}, wantCode: ExitOK, wantStopped: true, wantText: "Meltype を終了して"},
		{name: "--dry-run では終了させることを示すだけ", args: []string{"--dry-run"}, wantCode: ExitOK, wantText: "終了させて"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := runningEnv(t)
			if tt.stopAppOnYes != "" {
				l.run(t, false, "", "app", "config", "set", "stop-app-on-yes", tt.stopAppOnYes)
			}

			got := l.run(t, tt.interactive, tt.stdin, append([]string{"use", "v1.3.0", "--portable"}, tt.args...)...)

			if got.code != tt.wantCode {
				t.Fatalf("code = %d, want %d\n%s%s", got.code, tt.wantCode, got.stdout, got.stderr)
			}
			if !strings.Contains(got.stdout+got.stderr, tt.wantText) {
				t.Errorf("%q を示していない:\n%s%s", tt.wantText, got.stdout, got.stderr)
			}
			if tt.wantStopped {
				if l.stopped != 1 || l.started != 1 || l.read(t, "Meltype.exe") != "exe v1.3.0" {
					t.Errorf("終了、切り替え、起動し直しのどれかをしていない: stopped = %d, started = %d, Meltype.exe = %q", l.stopped, l.started, l.read(t, "Meltype.exe"))
				}
				return
			}
			if l.stopped != 0 || l.started != 0 || l.read(t, "Meltype.exe") != "exe v1.4.0" {
				t.Errorf("終了させないはずなのに変えた: stopped = %d, started = %d, Meltype.exe = %q", l.stopped, l.started, l.read(t, "Meltype.exe"))
			}
		})
	}
}

func TestStopAppGivesUpWithoutChanging(t *testing.T) {
	l := runningEnv(t)
	l.stopFails = true

	got := l.run(t, false, "", "use", "v1.3.0", "--portable", "-y")

	if got.code != ExitNeedsAction || !strings.Contains(got.stderr, "強制終了はしていません") {
		t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
	}
	if l.started != 0 || l.read(t, "Meltype.exe") != "exe v1.4.0" {
		t.Errorf("終了できなかったのに進めた: started = %d", l.started)
	}
}

func TestStopAppForOtherCommands(t *testing.T) {
	commands := map[string]func(t *testing.T, l *localEnv) []string{
		"rollback": func(t *testing.T, l *localEnv) []string {
			l.running = nil
			l.run(t, false, "", "use", "v1.3.0", "--portable", "-y")
			l.running = []string{filepath.Join(l.appDir(), "Meltype.exe")}
			return []string{"rollback", "-y"}
		},
		"snapshot create": func(t *testing.T, l *localEnv) []string {
			return []string{"snapshot", "create", "-y"}
		},
		"snapshot restore": func(t *testing.T, l *localEnv) []string {
			l.running = nil
			id := l.createSnapshot(t)
			l.running = []string{filepath.Join(l.appDir(), "Meltype.exe")}
			return []string{"snapshot", "restore", id, "-y"}
		},
	}
	for name, prepare := range commands {
		t.Run(name+" も -y で終了させ、終わったら起動し直す", func(t *testing.T) {
			l := runningEnv(t)
			args := prepare(t, l)

			got := l.run(t, false, "", args...)

			if got.code != ExitOK || l.stopped != 1 || l.started != 1 {
				t.Errorf("code = %d, stopped = %d, started = %d\n%s", got.code, l.stopped, l.started, got.stderr)
			}
		})
	}
}

func TestNoRestartWhenNotRunning(t *testing.T) {
	l := runningEnv(t)
	l.running = nil

	got := l.run(t, false, "", "use", "v1.3.0", "--portable", "-y")

	if got.code != ExitOK || l.stopped != 0 || l.started != 0 {
		t.Errorf("code = %d, stopped = %d, started = %d", got.code, l.stopped, l.started)
	}
}
