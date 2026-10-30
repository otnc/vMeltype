package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otnc/vmeltype/internal/store"
)

func TestRollbackReturnsToPreviousVersion(t *testing.T) {
	l := newLocalEnv(t)
	l.storePortable(t, "v1.4.0", nil)
	l.storePortable(t, "v1.3.0", nil)
	l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")
	l.run(t, false, "", "use", "v1.3.0", "--portable", "-y")
	os.MkdirAll(filepath.Join(l.appDir(), "data"), 0o700)
	os.WriteFile(filepath.Join(l.appDir(), "data", "userdict.txt"), []byte("1.3.0 で足した語"), 0o600)

	got := l.run(t, false, "", "rollback", "-y")

	if got.code != ExitOK || l.read(t, "Meltype.exe") != "exe v1.4.0" {
		t.Fatalf("code = %d, Meltype.exe = %q\n%s", got.code, l.read(t, "Meltype.exe"), got.stderr)
	}
	if l.read(t, "data/userdict.txt") != "1.3.0 で足した語" {
		t.Errorf("rollback で data を巻き戻した")
	}
	var current struct {
		Active   struct{ Tag string } `json:"active"`
		Previous struct{ Tag string } `json:"previous"`
	}
	json.Unmarshal([]byte(l.run(t, false, "", "current", "--json").stdout), &current)
	if current.Active.Tag != "v1.4.0" || current.Previous.Tag != "v1.3.0" {
		t.Errorf("記録が違う: %+v", current)
	}
}

func TestRollbackWithoutPreviousVersion(t *testing.T) {
	l := newLocalEnv(t)
	l.storePortable(t, "v1.4.0", nil)
	l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")

	got := l.run(t, false, "", "rollback", "-y")

	if got.code != ExitError || !strings.Contains(got.stderr, "直前") {
		t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
	}
}

func TestUninstall(t *testing.T) {
	setup := func(t *testing.T) *localEnv {
		l := newLocalEnv(t)
		l.storePortable(t, "v1.4.0", nil)
		l.storePortable(t, "v1.3.0", nil)
		l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")
		return l
	}
	isStored := func(l *localEnv, tag string) bool {
		_, err := store.Store{Root: l.root}.Get(store.ID(tag, "windows", "amd64", "portable"))
		return err == nil
	}

	t.Run("有効でない保持バージョンを削除する", func(t *testing.T) {
		l := setup(t)
		got := l.run(t, false, "", "un", "v1.3.0", "--portable", "-y")
		if got.code != ExitOK || isStored(l, "v1.3.0") {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
		if entries, _ := os.ReadDir(filepath.Join(l.root, "versions")); len(entries) != 1 {
			t.Errorf("削除の途中のディレクトリが残っている: %v", entries)
		}
		if l.read(t, "Meltype.exe") != "exe v1.4.0" {
			t.Errorf("実行先に触れた")
		}
	})
	t.Run("有効なバージョンは削除しない", func(t *testing.T) {
		l := setup(t)
		got := l.run(t, false, "", "uninstall", "v1.4.0", "--portable", "-y")
		if got.code != ExitError || !isStored(l, "v1.4.0") || !strings.Contains(got.stderr, "有効") {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
	t.Run("空の Enter では中止する", func(t *testing.T) {
		l := setup(t)
		got := l.run(t, true, "\n", "uninstall", "v1.3.0", "--portable")
		if got.code != ExitCanceled || !isStored(l, "v1.3.0") || !strings.Contains(got.stderr, "[y/N]") {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
	t.Run("y で削除する", func(t *testing.T) {
		l := setup(t)
		got := l.run(t, true, "y\n", "uninstall", "v1.3.0", "--portable")
		if got.code != ExitOK || isStored(l, "v1.3.0") {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
	t.Run("非 TTY で -y がなければ止まる", func(t *testing.T) {
		l := setup(t)
		got := l.run(t, false, "", "uninstall", "v1.3.0", "--portable")
		if got.code != ExitNeedsAction || !isStored(l, "v1.3.0") {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
	t.Run("保持していないバージョン", func(t *testing.T) {
		l := setup(t)
		got := l.run(t, false, "", "uninstall", "v9.9.9", "--portable", "-y")
		if got.code != ExitError {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
}
