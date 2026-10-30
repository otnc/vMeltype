package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/otnc/vmeltype/internal/lifecycle"
)

type pinRecord struct {
	StoreID string `json:"storeId"`
	Tag     string `json:"tag"`
	Variant string `json:"variant"`
}

// currentPin は、current --json が示す固定を返す。固定していなければ nil。
func (l *localEnv) currentPin(t *testing.T) *pinRecord {
	t.Helper()
	var current struct {
		Active struct{ Tag string } `json:"active"`
		Pin    *pinRecord           `json:"pin"`
	}
	got := l.run(t, false, "", "current", "--json")
	if err := json.Unmarshal([]byte(got.stdout), &current); err != nil {
		t.Fatalf("current --json を読めない: %v\n%s", err, got.stdout)
	}
	return current.Pin
}

// pinnedEnv は、v1.4.0 と v1.3.0 を保持し、v1.3.0 から v1.4.0 に切り替えて v1.4.0 に固定した状態を作る。
func pinnedEnv(t *testing.T) *localEnv {
	t.Helper()
	l := newLocalEnv(t)
	l.storePortable(t, "v1.4.0", nil)
	l.storePortable(t, "v1.3.0", nil)
	l.run(t, false, "", "use", "v1.3.0", "--portable", "-y")
	l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")
	if got := l.run(t, false, "", "pin"); got.code != ExitOK {
		t.Fatalf("pin できない: %s", got.stderr)
	}
	return l
}

func TestPin(t *testing.T) {
	t.Run("有効なバージョンのタグ、variant、保持 ID を記録する", func(t *testing.T) {
		l := pinnedEnv(t)

		pin := l.currentPin(t)
		if pin == nil || pin.Tag != "v1.4.0" || pin.Variant != "portable" || pin.StoreID == "" {
			t.Errorf("固定の記録が違う: %+v", pin)
		}
		if got := l.run(t, false, "", "current"); !strings.Contains(got.stdout, "固定") {
			t.Errorf("current が固定を示していない: %s", got.stdout)
		}
	})
	t.Run("有効なバージョンと合う指定なら固定する", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storePortable(t, "v1.4.0", nil)
		l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")

		got := l.run(t, false, "", "pin", "1.4.0")

		if got.code != ExitOK || l.currentPin(t) == nil {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
	t.Run("有効なバージョンと合わない指定は、固定せずに use を案内する", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storePortable(t, "v1.4.0", nil)
		l.storePortable(t, "v1.3.0", nil)
		l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")

		got := l.run(t, false, "", "pin", "v1.3.0")

		if got.code != ExitError || !strings.Contains(got.stderr, "use") || l.currentPin(t) != nil {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
	t.Run("有効なバージョンがなければ固定しない", func(t *testing.T) {
		l := newLocalEnv(t)

		got := l.run(t, false, "", "pin")

		if got.code != ExitError || l.currentPin(t) != nil {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
	t.Run("中断した変更が残っていれば固定しない", func(t *testing.T) {
		l := interruptedUse(t, lifecycle.PhaseNewPlaced)

		got := l.run(t, false, "", "pin")

		if got.code != ExitNeedsAction || l.currentPin(t) != nil {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
}

func TestPinBlocksSwitchingToAnotherVersion(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "use は別のバージョンに切り替えない", args: []string{"use", "v1.3.0", "--portable", "-y"}},
		{name: "rollback は直前のバージョンに戻さない", args: []string{"rollback", "-y"}},
		{name: "--dry-run でも固定を示して止める", args: []string{"use", "v1.3.0", "--portable", "--dry-run"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := pinnedEnv(t)

			got := l.run(t, false, "", tt.args...)

			if got.code != ExitNeedsAction || !strings.Contains(got.stderr, "unpin") || !strings.Contains(got.stderr, "--override-pin") {
				t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
			}
			if l.read(t, "Meltype.exe") != "exe v1.4.0" {
				t.Errorf("固定したバージョンから切り替えた: %q", l.read(t, "Meltype.exe"))
			}
		})
	}
}

func TestPinAllowsSameVersion(t *testing.T) {
	l := pinnedEnv(t)

	got := l.run(t, false, "", "use", "v1.4.0", "--portable", "--force", "-y")

	if got.code != ExitOK || l.read(t, "Meltype.exe") != "exe v1.4.0" {
		t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
	}
	if pin := l.currentPin(t); pin == nil || pin.Tag != "v1.4.0" {
		t.Errorf("再適用で固定が変わった: %+v", pin)
	}
}

func TestOverridePin(t *testing.T) {
	for _, args := range [][]string{
		{"use", "v1.3.0", "--portable", "--override-pin", "-y"},
		{"rollback", "--override-pin", "-y"},
	} {
		t.Run(args[0]+" は切り替えて、固定を新しいバージョンに移す", func(t *testing.T) {
			l := pinnedEnv(t)

			got := l.run(t, false, "", args...)

			if got.code != ExitOK || l.read(t, "Meltype.exe") != "exe v1.3.0" {
				t.Fatalf("code = %d, stderr = %s", got.code, got.stderr)
			}
			if pin := l.currentPin(t); pin == nil || pin.Tag != "v1.3.0" {
				t.Errorf("固定が新しいバージョンに移っていない: %+v", pin)
			}
		})
	}
}

func TestUnpin(t *testing.T) {
	t.Run("固定を解除すれば別のバージョンに切り替えられる", func(t *testing.T) {
		l := pinnedEnv(t)

		got := l.run(t, false, "", "unpin")

		if got.code != ExitOK || l.currentPin(t) != nil {
			t.Fatalf("code = %d, stderr = %s", got.code, got.stderr)
		}
		if l.read(t, "Meltype.exe") != "exe v1.4.0" {
			t.Errorf("unpin で有効なバージョンが変わった")
		}
		if next := l.run(t, false, "", "use", "v1.3.0", "--portable", "-y"); next.code != ExitOK {
			t.Errorf("解除のあとに切り替えられない: %s", next.stderr)
		}
	})
	t.Run("固定していなければ何もしない", func(t *testing.T) {
		got := newLocalEnv(t).run(t, false, "", "unpin")

		if got.code != ExitOK || !strings.Contains(got.stdout, "固定していません") {
			t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
		}
	})
}

func TestRecoverIgnoresPin(t *testing.T) {
	l := interruptedUse(t, lifecycle.PhaseNewPlaced)
	// pin は中断した変更が残っていると固定しないので、記録を直接作る
	if _, err := lifecycle.Pin(l.root, time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	got := l.run(t, false, "", "recover", "-y")

	if got.code != ExitOK || l.read(t, "Meltype.exe") != "exe v1.3.0" {
		t.Errorf("固定が recover を止めた: code = %d, stderr = %s", got.code, got.stderr)
	}
}
