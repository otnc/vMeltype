package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otnc/vmeltype/internal/lifecycle"
)

func TestRun(t *testing.T) {
	t.Run("有効なバージョンの Meltype を起動する", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storePortable(t, "v1.4.0", nil)
		l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")

		got := l.run(t, false, "", "run")

		if got.code != ExitOK || l.started != 1 {
			t.Errorf("code = %d, started = %d, stderr = %s", got.code, l.started, got.stderr)
		}
	})
	t.Run("すでに動いていれば、起動し直さない", func(t *testing.T) {
		l := runningEnv(t)

		got := l.run(t, false, "", "run")

		if got.code != ExitOK || l.started != 0 || !strings.Contains(got.stdout, "動いています") {
			t.Errorf("code = %d, started = %d\n%s%s", got.code, l.started, got.stdout, got.stderr)
		}
	})

	tests := []struct {
		name     string
		prepare  func(t *testing.T) *localEnv
		wantCode int
		wantText string
	}{
		{
			name:     "有効なバージョンがなければ use を案内する",
			prepare:  newLocalEnv,
			wantCode: ExitError,
			wantText: "vmeltype use",
		},
		{
			name:     "中断した変更があれば起動しない",
			prepare:  func(t *testing.T) *localEnv { return interruptedUse(t, lifecycle.PhaseOldMoved) },
			wantCode: ExitNeedsAction,
			wantText: "recover",
		},
		{
			name: "実行先に Meltype.exe がなければ、置き直し方を示す",
			prepare: func(t *testing.T) *localEnv {
				l := newLocalEnv(t)
				l.storePortable(t, "v1.4.0", nil)
				l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")
				os.Remove(filepath.Join(l.appDir(), "Meltype.exe"))
				return l
			},
			wantCode: ExitError,
			wantText: "--force",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := tt.prepare(t)

			got := l.run(t, false, "", "run")

			if got.code != tt.wantCode || !strings.Contains(got.stderr, tt.wantText) {
				t.Errorf("code = %d, want %d\n%s", got.code, tt.wantCode, got.stderr)
			}
			if l.started != 0 {
				t.Errorf("起動しないはずなのに起動した")
			}
		})
	}
}
