package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otnc/vmeltype/internal/lifecycle"
)

type statusJSON struct {
	Active     *struct{ Tag string }   `json:"active"`
	Previous   *struct{ Tag string }   `json:"previous"`
	Pin        *struct{ Tag string }   `json:"pin"`
	Deployment string                  `json:"deployment"`
	Mismatched []string                `json:"mismatched"`
	Running    []string                `json:"running"`
	DataDir    string                  `json:"dataDir"`
	DataExists bool                    `json:"dataExists"`
	Pending    *struct{ Phase string } `json:"pending"`
}

func (l *localEnv) status(t *testing.T) statusJSON {
	t.Helper()
	got := l.run(t, false, "", "status", "--json")
	if got.code != ExitOK {
		t.Fatalf("status --json: code = %d, stderr = %s", got.code, got.stderr)
	}
	var s statusJSON
	if err := json.Unmarshal([]byte(got.stdout), &s); err != nil {
		t.Fatalf("JSON を読めない: %v\n%s", err, got.stdout)
	}
	return s
}

func TestStatus(t *testing.T) {
	t.Run("有効なバージョンがなければ、そう示す", func(t *testing.T) {
		l := newLocalEnv(t)

		got := l.run(t, false, "", "status")

		if got.code != ExitOK || !strings.Contains(got.stdout, "有効なバージョンはありません") {
			t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
		}
		if s := l.status(t); s.Active != nil || s.Deployment != "none" {
			t.Errorf("JSON が違う: %+v", s)
		}
	})
	t.Run("有効なバージョン、直前のバージョン、固定、配置、稼働、データ先を示す", func(t *testing.T) {
		l := pinnedEnv(t)
		l.writeData(t, "userdict.txt", "辞書")
		l.running = []string{filepath.Join(l.appDir(), "Meltype.exe")}

		got := l.run(t, false, "", "status")

		for _, want := range []string{"v1.4.0", "v1.3.0", "固定", "一致", "動いています", filepath.Join(l.appDir(), "data")} {
			if !strings.Contains(got.stdout, want) {
				t.Errorf("%q を示していない:\n%s", want, got.stdout)
			}
		}
		s := l.status(t)
		if s.Active.Tag != "v1.4.0" || s.Previous.Tag != "v1.3.0" || s.Pin.Tag != "v1.4.0" || s.Deployment != "match" || len(s.Running) != 1 || !s.DataExists || s.Pending != nil {
			t.Errorf("JSON が違う: %+v", s)
		}
	})
	t.Run("実行先のファイルが保持バージョンと違えば、置き直し方を示す", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storePortable(t, "v1.4.0", nil)
		l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")
		os.WriteFile(filepath.Join(l.appDir(), "Meltype.exe"), []byte("書き換えた"), 0o600)
		os.Remove(filepath.Join(l.appDir(), "dotnet", "host.dll"))

		got := l.run(t, false, "", "status")

		if !strings.Contains(got.stdout, "--force") {
			t.Errorf("置き直し方を示していない:\n%s", got.stdout)
		}
		s := l.status(t)
		if s.Deployment != "mismatch" || strings.Join(s.Mismatched, ",") != "Meltype.exe,dotnet/host.dll" {
			t.Errorf("JSON が違う: %+v", s)
		}
	})
	t.Run("中断した変更があれば、recover を案内する", func(t *testing.T) {
		l := interruptedUse(t, lifecycle.PhaseOldMoved)

		got := l.run(t, false, "", "status")

		if got.code != ExitOK || !strings.Contains(got.stdout, "recover") {
			t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
		}
		if s := l.status(t); s.Pending == nil || s.Pending.Phase != lifecycle.PhaseOldMoved {
			t.Errorf("JSON が違う: %+v", s)
		}
	})
}
