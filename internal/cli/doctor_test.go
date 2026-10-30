package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otnc/vmeltype/internal/lifecycle"
	"github.com/otnc/vmeltype/internal/store"
)

type doctorJSON struct {
	OK     bool `json:"ok"`
	Checks []struct {
		Name    string `json:"name"`
		Level   string `json:"level"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	} `json:"checks"`
}

// levels は、診断の項目ごとの結果を返す。
func (d doctorJSON) levels() map[string]string {
	m := map[string]string{}
	for _, c := range d.Checks {
		m[c.Name] = c.Level
	}
	return m
}

func (l *localEnv) doctor(t *testing.T) (result, doctorJSON) {
	t.Helper()
	got := l.run(t, false, "", "doctor", "--json")
	var d doctorJSON
	if err := json.Unmarshal([]byte(got.stdout), &d); err != nil {
		t.Fatalf("JSON を読めない: %v\n%s%s", err, got.stdout, got.stderr)
	}
	return got, d
}

func TestDoctorWithoutAnything(t *testing.T) {
	l := newLocalEnv(t)

	got := l.run(t, false, "", "doctor")

	if got.code != ExitOK || !strings.Contains(got.stdout, "有効なバージョンはありません") {
		t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	if _, err := os.Stat(l.root); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(l.root); len(entries) != 0 {
		t.Errorf("診断で管理ルートに書いた: %v", entries)
	}
}

func TestDoctorHealthy(t *testing.T) {
	l := pinnedEnv(t)

	got := l.run(t, false, "", "doctor")

	if got.code != ExitOK || strings.Contains(got.stdout, "要対応") || strings.Contains(got.stdout, "警告") {
		t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	if _, d := l.doctor(t); !d.OK || len(d.Checks) == 0 {
		t.Errorf("JSON が違う: %+v", d)
	}
}

func TestDoctorFindsProblems(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(t *testing.T) *localEnv
		check     string
		wantLevel string
		wantCode  int
		wantHint  string
	}{
		{
			name:      "中断した変更があれば、recover を案内する",
			prepare:   func(t *testing.T) *localEnv { return interruptedUse(t, lifecycle.PhaseOldMoved) },
			check:     "journal",
			wantLevel: "fail",
			wantCode:  ExitNeedsAction,
			wantHint:  "vmeltype recover",
		},
		{
			name: "実行先が保持バージョンと違えば、置き直し方を示す",
			prepare: func(t *testing.T) *localEnv {
				l := pinnedEnv(t)
				os.WriteFile(filepath.Join(l.appDir(), "Meltype.exe"), []byte("書き換えた"), 0o600)
				return l
			},
			check:     "deployment",
			wantLevel: "fail",
			wantCode:  ExitNeedsAction,
			wantHint:  "--force",
		},
		{
			name: "直前のバージョンを保持していなければ、rollback できないと警告する",
			prepare: func(t *testing.T) *localEnv {
				l := newLocalEnv(t)
				l.storePortable(t, "v1.4.0", nil)
				l.storePortable(t, "v1.3.0", nil)
				l.run(t, false, "", "use", "v1.3.0", "--portable", "-y")
				l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")
				l.run(t, false, "", "uninstall", "v1.3.0", "--portable", "-y")
				return l
			},
			check:     "previous",
			wantLevel: "warn",
			wantCode:  ExitOK,
			wantHint:  "vmeltype install v1.3.0 --portable",
		},
		{
			name: "読めない保持バージョンがあれば警告する",
			prepare: func(t *testing.T) *localEnv {
				l := pinnedEnv(t)
				broken := store.Store{Root: l.root}.Path(store.ID("v1.2.0", "windows", "amd64", "portable"))
				os.MkdirAll(broken, 0o700)
				os.WriteFile(filepath.Join(broken, "manifest.json"), []byte("{"), 0o600)
				return l
			},
			check:     "stored",
			wantLevel: "warn",
			wantCode:  ExitOK,
			wantHint:  "v1.2.0",
		},
		{
			name: "読めないスナップショットがあれば警告する",
			prepare: func(t *testing.T) *localEnv {
				l := pinnedEnv(t)
				os.MkdirAll(filepath.Join(l.root, "snapshots", "20261029T090000Z"), 0o700)
				return l
			},
			check:     "snapshots",
			wantLevel: "warn",
			wantCode:  ExitOK,
			wantHint:  "20261029T090000Z",
		},
		{
			name: "設定を読めなければ、直し方を示す",
			prepare: func(t *testing.T) *localEnv {
				l := pinnedEnv(t)
				os.WriteFile(filepath.Join(l.root, "config.json"), []byte(`{"stop-app-on-yes": "はい"}`), 0o600)
				return l
			},
			check:     "config",
			wantLevel: "fail",
			wantCode:  ExitNeedsAction,
			wantHint:  "app config set",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := tt.prepare(t)

			got, d := l.doctor(t)

			if got.code != tt.wantCode {
				t.Errorf("code = %d, want %d\n%s", got.code, tt.wantCode, got.stderr)
			}
			if level := d.levels()[tt.check]; level != tt.wantLevel {
				t.Errorf("%s の結果 = %q, want %q: %+v", tt.check, level, tt.wantLevel, d)
			}
			if d.OK != (tt.wantLevel != "fail") {
				t.Errorf("ok = %v", d.OK)
			}
			if text := l.run(t, false, "", "doctor"); !strings.Contains(text.stdout, tt.wantHint) {
				t.Errorf("%q を示していない:\n%s", tt.wantHint, text.stdout)
			}
		})
	}
}

func TestDoctorChecksIMEDLL(t *testing.T) {
	tests := []struct {
		name      string
		installed map[string]string
		wantLevel string
		wantCode  int
	}{
		{name: "有効なバージョンの DLL なら OK", installed: map[string]string{"x64": "HASH-X64-V1.4.0", "x86": "HASH-X86-V1.4.0"}, wantLevel: "ok", wantCode: ExitOK},
		{name: "ほかのバージョンの DLL なら、置き直し方を示す", installed: map[string]string{"x64": "HASH-X64-V1.3.0", "x86": "HASH-X86-V1.3.0"}, wantLevel: "fail", wantCode: ExitNeedsAction},
		{name: "DLL のハッシュがなければ、確かめられないと警告する", installed: map[string]string{"x64": "", "x86": ""}, wantLevel: "warn", wantCode: ExitOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := managedStandard(t)
			l.imeRegistered = true
			l.installedTip = tt.installed

			got, d := l.doctor(t)

			if got.code != tt.wantCode || d.levels()["ime"] != tt.wantLevel {
				t.Errorf("code = %d, ime = %q\n%s", got.code, d.levels()["ime"], got.stderr)
			}
			if tt.wantLevel == "fail" && !strings.Contains(l.run(t, false, "", "doctor").stdout, "vmeltype use v1.4.0 --force") {
				t.Errorf("置き直し方を示していない")
			}
		})
	}
	t.Run("IME を登録していなければ調べない", func(t *testing.T) {
		l := managedStandard(t)

		_, d := l.doctor(t)

		if _, ok := d.levels()["ime"]; ok {
			t.Errorf("登録していないのに調べた: %+v", d)
		}
	})
}
