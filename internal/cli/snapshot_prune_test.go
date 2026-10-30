package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/otnc/vmeltype/internal/snapshot"
)

// switchingStandard は、通常版の v1.3.0 と v1.4.0 を保持し、本家を入れた状態を作る。use のたびに、自動のスナップショットが 1 つ増える。
func switchingStandard(t *testing.T, keep string) *localEnv {
	t.Helper()
	l := newLocalEnv(t)
	l.storeStandard(t, "v1.3.0")
	l.storeStandard(t, "v1.4.0")
	l.installUpstream(t)
	l.setConfig(t, "auto-snapshot-keep", keep)
	return l
}

func (l *localEnv) setConfig(t *testing.T, key, value string) {
	t.Helper()
	if got := l.run(t, false, "", "app", "config", "set", key, value); got.code != ExitOK {
		t.Fatal(got.stderr)
	}
}

func (l *localEnv) switchTimes(t *testing.T, n int) {
	t.Helper()
	for i := range n {
		tag := []string{"1.3.0", "1.4.0"}[i%2]
		if got := l.run(t, false, "", "use", tag, "-y"); got.code != ExitOK {
			t.Fatal(got.stderr)
		}
	}
}

// snapshots は、スナップショットの ID を理由ごとに、新しい順で返す。
func (l *localEnv) snapshots(t *testing.T) map[snapshot.Reason][]string {
	t.Helper()
	var listed struct{ Snapshots []snapshot.Manifest }
	json.Unmarshal([]byte(l.run(t, false, "", "snapshot", "list", "--json").stdout), &listed)
	got := map[snapshot.Reason][]string{}
	for _, m := range listed.Snapshots {
		got[m.Reason] = append(got[m.Reason], m.ID)
	}
	return got
}

func TestAutoSnapshotsArePruned(t *testing.T) {
	t.Run("切り替えの前に作ったものは、設定の数だけ残す", func(t *testing.T) {
		l := switchingStandard(t, "2")
		l.switchTimes(t, 3)
		manual := l.createSnapshot(t)

		l.run(t, false, "", "use", "1.4.0", "-y")

		got := l.snapshots(t)
		if len(got[snapshot.ReasonBeforeUse]) != 2 || !slices.Contains(got[snapshot.ReasonManual], manual) {
			t.Errorf("自動のものが 2 つにならないか、手動のものを消した: %v", got)
		}
	})
	t.Run("0 なら消さない", func(t *testing.T) {
		l := switchingStandard(t, "0")

		l.switchTimes(t, 4)

		if got := l.snapshots(t); len(got[snapshot.ReasonBeforeUse]) != 4 {
			t.Errorf("消さない設定なのに消した: %v", got)
		}
	})
	t.Run("いま復元したものは消さない", func(t *testing.T) {
		l := switchingStandard(t, "2")
		l.switchTimes(t, 2)
		autos := l.snapshots(t)[snapshot.ReasonBeforeUse]
		oldest := autos[len(autos)-1]
		l.setConfig(t, "auto-snapshot-keep", "1")

		if got := l.run(t, false, "", "snapshot", "restore", oldest, "-y"); got.code != ExitOK {
			t.Fatal(got.stderr)
		}

		got := l.snapshots(t)
		if !slices.Contains(got[snapshot.ReasonBeforeUse], oldest) || len(got[snapshot.ReasonBeforeRestore]) != 1 {
			t.Errorf("いま復元したものか、復元の前に作ったものを消した: %v", got)
		}
		if slices.Contains(got[snapshot.ReasonBeforeUse], autos[0]) {
			t.Errorf("上限を超えた古いものを消していない: %v", got)
		}
	})
}

func TestSnapshotRemove(t *testing.T) {
	l := managedStandard(t)
	id := l.createSnapshot(t)

	for _, args := range [][]string{{"--dry-run"}, {}} {
		l.run(t, false, "", append([]string{"snapshot", "remove", id}, args...)...)
	}
	if got := l.snapshots(t); !slices.Contains(got[snapshot.ReasonManual], id) {
		t.Fatalf("--dry-run か、確認できないときに消した: %v", got)
	}

	got := l.run(t, false, "", "snapshot", "remove", id, "-y")

	if got.code != ExitOK || slices.Contains(l.snapshots(t)[snapshot.ReasonManual], id) {
		t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

func TestSnapshotPrune(t *testing.T) {
	t.Run("--keep の数だけ自動のものを残し、手動のものは消さない", func(t *testing.T) {
		l := switchingStandard(t, "0")
		l.switchTimes(t, 3)
		manual := l.createSnapshot(t)
		autos := l.snapshots(t)[snapshot.ReasonBeforeUse]

		dry := l.run(t, false, "", "snapshot", "prune", "--keep", "1", "--dry-run")
		if dry.code != ExitOK || !strings.Contains(dry.stdout, autos[2]) || len(l.snapshots(t)[snapshot.ReasonBeforeUse]) != 3 {
			t.Fatalf("--dry-run で消す対象を示さないか、消した: code = %d\n%s%s", dry.code, dry.stdout, dry.stderr)
		}

		got := l.run(t, false, "", "snapshot", "prune", "--keep", "1", "-y")

		left := l.snapshots(t)
		if got.code != ExitOK || !slices.Equal(left[snapshot.ReasonBeforeUse], autos[:1]) || !slices.Contains(left[snapshot.ReasonManual], manual) {
			t.Errorf("code = %d, 残ったもの = %v\n%s", got.code, left, got.stderr)
		}
	})
	t.Run("--keep がなければ設定の数を使う", func(t *testing.T) {
		l := switchingStandard(t, "0")
		l.switchTimes(t, 3)
		l.setConfig(t, "auto-snapshot-keep", "2")

		l.run(t, false, "", "snapshot", "prune", "-y")

		if got := l.snapshots(t); len(got[snapshot.ReasonBeforeUse]) != 2 {
			t.Errorf("設定の数にならない: %v", got)
		}
	})
}
