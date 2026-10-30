package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otnc/vmeltype/internal/lifecycle"
	"github.com/otnc/vmeltype/internal/store"
)

// interruptedUse は、v1.4.0 が有効で data がある状態から、v1.3.0 への use が phase で止まった状態を作る。
func interruptedUse(t *testing.T, phase string) *localEnv {
	t.Helper()
	l := newLocalEnv(t)
	l.storePortable(t, "v1.4.0", nil)
	l.storePortable(t, "v1.3.0", nil)
	if got := l.run(t, false, "", "use", "v1.4.0", "--portable", "-y"); got.code != ExitOK {
		t.Fatal(got.stderr)
	}
	os.MkdirAll(filepath.Join(l.appDir(), "data"), 0o700)
	os.WriteFile(filepath.Join(l.appDir(), "data", "userdict.txt"), []byte("辞書"), 0o600)

	parent := filepath.Dir(l.appDir())
	staging := filepath.Join(parent, ".staging-1")
	backup := filepath.Join(parent, ".previous-1")
	target, err := store.Store{Root: l.root}.Get(store.ID("v1.3.0", "windows", "amd64", "portable"))
	if err != nil {
		t.Fatal(err)
	}
	copyTree(t, filepath.Join(store.Store{Root: l.root}.Path(target.ID), "files"), staging)

	state, _ := lifecycle.LoadState(l.root)
	journal := lifecycle.Journal{
		SchemaVersion: lifecycle.SchemaVersion,
		Operation:     "use",
		StartedAt:     time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC),
		Phase:         phase,
		From:          state.Active,
		To: lifecycle.Activation{
			StoreID: target.ID, Tag: target.Tag, Variant: target.Variant, AppDir: l.appDir(),
			OwnedEntries: []string{"Meltype.exe", "Meltype.portable", "dotnet"},
		},
		StagingDir: staging,
		BackupDir:  backup,
		Carried:    []string{"data"},
	}
	switch phase {
	case lifecycle.PhaseOldMoved:
		os.Rename(l.appDir(), backup)
	case lifecycle.PhaseNewPlaced:
		os.Rename(l.appDir(), backup)
		os.Rename(staging, l.appDir())
	}
	data, _ := json.Marshal(journal)
	os.MkdirAll(filepath.Join(l.root, "transactions"), 0o700)
	if err := os.WriteFile(filepath.Join(l.root, "transactions", "current.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return l
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRecover(t *testing.T) {
	tests := []struct {
		name       string
		phase      string
		wantActive string
	}{
		{name: "組み立ての段階で止まっていれば、何も変えずに後片付けする", phase: lifecycle.PhaseStaged, wantActive: "v1.4.0"},
		{name: "前の実行先を退避した段階で止まっていれば、元へ戻す", phase: lifecycle.PhaseOldMoved, wantActive: "v1.4.0"},
		{name: "新しいバージョンを置いた段階で止まっていれば、引き継ぎを終える", phase: lifecycle.PhaseNewPlaced, wantActive: "v1.3.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := interruptedUse(t, tt.phase)

			got := l.run(t, false, "", "recover", "-y")

			if got.code != ExitOK {
				t.Fatalf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
			}
			if want := "exe " + tt.wantActive; l.read(t, "Meltype.exe") != want {
				t.Errorf("Meltype.exe = %q, want %q", l.read(t, "Meltype.exe"), want)
			}
			if l.read(t, "data/userdict.txt") != "辞書" {
				t.Errorf("data が実行先にない")
			}
			if current := l.run(t, false, "", "current"); !strings.Contains(current.stdout, tt.wantActive) {
				t.Errorf("有効なバージョンの記録が違う: %s", current.stdout)
			}
			entries, _ := os.ReadDir(filepath.Dir(l.appDir()))
			if len(entries) != 1 {
				t.Errorf("作業用や退避のディレクトリが残っている: %v", entries)
			}
			if next := l.run(t, false, "", "use", "v1.3.0", "--portable", "-y"); next.code != ExitOK {
				t.Errorf("復旧のあとに use できない: %s", next.stderr)
			}
		})
	}
}

func TestRecoverWithoutPendingChange(t *testing.T) {
	got := newLocalEnv(t).run(t, false, "", "recover")

	if got.code != ExitOK || !strings.Contains(got.stdout, "ありません") {
		t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

func TestRecoverNeedsConfirmation(t *testing.T) {
	l := interruptedUse(t, lifecycle.PhaseOldMoved)

	got := l.run(t, false, "", "recover")

	if got.code != ExitNeedsAction {
		t.Errorf("code = %d\n%s", got.code, got.stderr)
	}
	if _, err := os.Stat(l.appDir()); err == nil {
		t.Errorf("確認の前に復旧した")
	}
}
