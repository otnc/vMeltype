package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otnc/vmeltype/internal/snapshot"
)

func (l *localEnv) writeData(t *testing.T, name, body string) {
	t.Helper()
	path := filepath.Join(l.appDir(), "data", filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newPortableWithData(t *testing.T) *localEnv {
	t.Helper()
	l := newLocalEnv(t)
	l.storePortable(t, "v1.4.0", nil)
	if got := l.run(t, false, "", "use", "1.4.0", "--portable", "-y"); got.code != ExitOK {
		t.Fatalf("use: %s", got.stderr)
	}
	l.writeData(t, "userdict.txt", "保存した辞書")
	return l
}

func (l *localEnv) createSnapshot(t *testing.T) string {
	t.Helper()
	got := l.run(t, false, "", "snapshot", "create", "--json")
	if got.code != ExitOK {
		t.Fatalf("snapshot create: %d %s", got.code, got.stderr)
	}
	var m snapshot.Manifest
	if err := json.Unmarshal([]byte(got.stdout), &m); err != nil {
		t.Fatalf("JSON を読めない: %v\n%s", err, got.stdout)
	}
	return m.ID
}

func TestSnapshotCreateAndList(t *testing.T) {
	l := newPortableWithData(t)

	created := l.run(t, false, "", "snapshot", "create")
	listed := l.run(t, false, "", "snapshot", "list")

	if created.code != ExitOK {
		t.Fatalf("code = %d, stderr = %s", created.code, created.stderr)
	}
	if listed.code != ExitOK || !strings.Contains(listed.stdout, "v1.4.0") || !strings.Contains(listed.stdout, "手動") {
		t.Errorf("一覧に作ったスナップショットがない:\n%s%s", listed.stdout, listed.stderr)
	}
}

func TestSnapshotCreateStopsWhileMeltypeRuns(t *testing.T) {
	l := newPortableWithData(t)
	l.running = []string{filepath.Join(l.appDir(), "Meltype.exe")}

	got := l.run(t, false, "", "snapshot", "create")

	if got.code != ExitNeedsAction {
		t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
	}
	if listed, _, _ := snapshot.List(l.root); len(listed) != 0 {
		t.Errorf("Meltype が動いている間に保存した")
	}
}

func TestSnapshotCreateWithoutPortableData(t *testing.T) {
	got := newLocalEnv(t).run(t, false, "", "snapshot", "create")

	if got.code != ExitError || !strings.Contains(got.stderr, "data") {
		t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
	}
}

func TestSnapshotRestore(t *testing.T) {
	t.Run("保存した時点に戻し、いまのデータも先に保存する", func(t *testing.T) {
		l := newPortableWithData(t)
		id := l.createSnapshot(t)
		l.writeData(t, "userdict.txt", "あとで変えた辞書")

		got := l.run(t, false, "", "snapshot", "restore", id, "-y")

		if got.code != ExitOK || l.read(t, "data/userdict.txt") != "保存した辞書" {
			t.Fatalf("code = %d, userdict = %q, stderr = %s", got.code, l.read(t, "data/userdict.txt"), got.stderr)
		}
		listed, _, _ := snapshot.List(l.root)
		if len(listed) != 2 || listed[0].Reason != snapshot.ReasonBeforeRestore {
			t.Fatalf("復元の前のデータを保存していない: %+v", listed)
		}
		back := l.run(t, false, "", "snapshot", "restore", listed[0].ID, "-y")
		if back.code != ExitOK || l.read(t, "data/userdict.txt") != "あとで変えた辞書" {
			t.Errorf("復元の前のデータに戻せない: %d %s", back.code, back.stderr)
		}
	})

	tests := []struct {
		name        string
		interactive bool
		stdin       string
		extra       []string
		running     bool
		wantCode    int
	}{
		{name: "対話で空の Enter なら中止する", interactive: true, stdin: "\n", wantCode: ExitCanceled},
		{name: "非 TTY で -y がなければ止まる", wantCode: ExitNeedsAction},
		{name: "--dry-run は変えない", extra: []string{"--dry-run"}, wantCode: ExitOK},
		{name: "動いている Meltype が終わらなければ止まる", extra: []string{"-y"}, running: true, wantCode: ExitNeedsAction},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newPortableWithData(t)
			id := l.createSnapshot(t)
			l.writeData(t, "userdict.txt", "いまの辞書")
			if tt.running {
				l.running = []string{filepath.Join(l.appDir(), "Meltype.exe")}
				l.stopFails = true
			}

			got := l.run(t, tt.interactive, tt.stdin, append([]string{"snapshot", "restore", id}, tt.extra...)...)

			if got.code != tt.wantCode {
				t.Errorf("code = %d, want %d (stderr: %s)", got.code, tt.wantCode, got.stderr)
			}
			if l.read(t, "data/userdict.txt") != "いまの辞書" {
				t.Errorf("データを変えた")
			}
			if listed, _, _ := snapshot.List(l.root); len(listed) != 1 {
				t.Errorf("確認の前に復元の前の保存をした: %d 件", len(listed))
			}
		})
	}
}

func TestSnapshotVerifyAndInspect(t *testing.T) {
	l := newPortableWithData(t)
	id := l.createSnapshot(t)

	inspected := l.run(t, false, "", "snapshot", "inspect", id)
	if inspected.code != ExitOK || !strings.Contains(inspected.stdout, "userdict.txt") {
		t.Errorf("inspect にファイルがない:\n%s%s", inspected.stdout, inspected.stderr)
	}
	if got := l.run(t, false, "", "snapshot", "verify", id); got.code != ExitOK {
		t.Errorf("verify: %d %s", got.code, got.stderr)
	}

	os.WriteFile(filepath.Join(snapshot.Path(l.root, id), "files", "userdict.txt"), []byte("壊れた"), 0o600)
	if got := l.run(t, false, "", "snapshot", "verify", id); got.code != ExitError || !strings.Contains(got.stderr, "userdict.txt") {
		t.Errorf("壊れたスナップショットを見逃した: %d %s", got.code, got.stderr)
	}
	if got := l.run(t, false, "", "snapshot", "restore", id, "-y"); got.code != ExitError || l.read(t, "data/userdict.txt") != "保存した辞書" {
		t.Errorf("壊れたスナップショットで復元した: %d %s", got.code, got.stderr)
	}
}

func TestSnapshotUnknownID(t *testing.T) {
	got := newLocalEnv(t).run(t, false, "", "snapshot", "inspect", "20260101T000000Z")

	if got.code != ExitError {
		t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
	}
}

func TestSnapshotExportImport(t *testing.T) {
	t.Run("書き出したものを別の PC で取り込み、復元できる", func(t *testing.T) {
		from := managedStandard(t)
		from.writeStandardData(t, "userdict.txt", "前の PC の辞書")
		id := from.createSnapshot(t)
		file := filepath.Join(t.TempDir(), "data.vmeltype-snapshot")

		exported := from.run(t, false, "", "snapshot", "export", id, file)
		if exported.code != ExitOK || !strings.Contains(exported.stdout, "辞書") {
			t.Fatalf("code = %d\n%s%s", exported.code, exported.stdout, exported.stderr)
		}

		to := managedStandard(t)
		imported := to.run(t, false, "", "snapshot", "import", file, "--json")
		var m struct{ ID string }
		json.Unmarshal([]byte(imported.stdout), &m)
		if imported.code != ExitOK || m.ID == "" {
			t.Fatalf("code = %d\n%s%s", imported.code, imported.stdout, imported.stderr)
		}
		restored := to.run(t, false, "", "snapshot", "restore", m.ID, "-y")
		if restored.code != ExitOK || readFile(filepath.Join(to.standardDataDir(), "userdict.txt")) != "前の PC の辞書" {
			t.Errorf("code = %d\n%s", restored.code, restored.stderr)
		}
	})
	t.Run("通常版のデータを portable 版には復元しない", func(t *testing.T) {
		from := managedStandard(t)
		id := from.createSnapshot(t)
		file := filepath.Join(t.TempDir(), "data.vmeltype-snapshot")
		from.run(t, false, "", "snapshot", "export", id, file)
		to := newPortableWithData(t)
		var m struct{ ID string }
		json.Unmarshal([]byte(to.run(t, false, "", "snapshot", "import", file, "--json").stdout), &m)

		got := to.run(t, false, "", "snapshot", "restore", m.ID, "-y")

		if got.code != ExitError || !strings.Contains(got.stderr, "いまの有効なバージョンは portable 版") || portableUserdict(to) != "保存した辞書" {
			t.Errorf("code = %d\n%s", got.code, got.stderr)
		}
	})
	t.Run("取り込んだものは、一覧と詳細で分かる", func(t *testing.T) {
		from := managedStandard(t)
		id := from.createSnapshot(t)
		file := filepath.Join(t.TempDir(), "data.vmeltype-snapshot")
		from.run(t, false, "", "snapshot", "export", id, file)
		to := managedStandard(t)
		made := to.createSnapshot(t)
		var m struct{ ID string }
		json.Unmarshal([]byte(to.run(t, false, "", "snapshot", "import", file, "--json").stdout), &m)

		list := to.run(t, false, "", "snapshot", "list").stdout
		if !strings.Contains(lineWith(list, m.ID), "取り込み") || strings.Contains(lineWith(list, made), "取り込み") {
			t.Errorf("取り込んだものが一覧で分からない:\n%s", list)
		}
		if inspect := to.run(t, false, "", "snapshot", "inspect", m.ID).stdout; !strings.Contains(inspect, "取り込んだ日時") {
			t.Errorf("取り込んだ日時を示していない:\n%s", inspect)
		}
	})
	t.Run("有効なバージョンがなければ、通常版のデータを復元せずに use を案内する", func(t *testing.T) {
		from := managedStandard(t)
		id := from.createSnapshot(t)
		file := filepath.Join(t.TempDir(), "data.vmeltype-snapshot")
		from.run(t, false, "", "snapshot", "export", id, file)
		to := newLocalEnv(t)
		var m struct{ ID string }
		json.Unmarshal([]byte(to.run(t, false, "", "snapshot", "import", file, "--json").stdout), &m)

		got := to.run(t, false, "", "snapshot", "restore", m.ID, "-y")

		if got.code != ExitError || !strings.Contains(got.stderr, "有効なバージョンがない") || !strings.Contains(got.stderr, "vmeltype use") || strings.Contains(got.stderr, "いまの有効なバージョンは") {
			t.Errorf("code = %d\n%s", got.code, got.stderr)
		}
	})
	t.Run("通常版の復元の計画では、data でなくデータと書く", func(t *testing.T) {
		l := managedStandard(t)
		id := l.createSnapshot(t)

		got := l.run(t, false, "", "snapshot", "restore", id, "--dry-run")

		if got.code != ExitOK || !strings.Contains(got.stdout, "いまのデータは") {
			t.Errorf("code = %d\n%s", got.code, got.stdout)
		}
	})
}

func (l *localEnv) writeStandardData(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(l.standardDataDir(), name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// portableUserdict は、portable 版の data の userdict.txt の中身を返す。
func portableUserdict(l *localEnv) string {
	return readFile(filepath.Join(l.appDir(), "data", "userdict.txt"))
}

// lineWith は、text のうち substr を含む最初の行を返す。
func lineWith(text, substr string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return ""
}
