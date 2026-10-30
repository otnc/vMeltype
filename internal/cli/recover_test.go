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
	"github.com/otnc/vmeltype/internal/winime"
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

// interruptedStandardUse は、通常版の v1.3.0 が有効で Meltype IME を登録済みの状態から、DLL を入れ替えたあとの v1.4.0 への use が phase で止まった状態を作る。
func interruptedStandardUse(t *testing.T, phase string) *localEnv {
	t.Helper()
	l := newLocalEnv(t)
	l.storeStandard(t, "v1.3.0")
	l.storeStandard(t, "v1.4.0")
	l.imeRegistered = true
	l.imeResult = winime.ResultReplaced
	if got := l.run(t, false, "", "use", "1.3.0", "-y"); got.code != ExitOK {
		t.Fatal(got.stderr)
	}

	appDir := l.standardAppDir()
	parent := filepath.Dir(appDir)
	staging := filepath.Join(parent, ".staging-1")
	backup := filepath.Join(parent, ".previous-1")
	target, err := store.Store{Root: l.root}.Get(store.ID("v1.4.0", "windows", "amd64", "standard"))
	if err != nil {
		t.Fatal(err)
	}
	copyTree(t, filepath.Join(store.Store{Root: l.root}.Path(target.ID), "files", "app"), staging)

	state, _ := lifecycle.LoadState(l.root)
	journal := lifecycle.Journal{
		SchemaVersion: lifecycle.SchemaVersion,
		Operation:     "use",
		StartedAt:     time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC),
		Phase:         phase,
		From:          state.Active,
		To: lifecycle.Activation{
			StoreID: target.ID, Tag: target.Tag, Variant: target.Variant, AppDir: appDir,
			OwnedEntries: []string{"Meltype.exe", "dotnet", "tip", "uninstall-helper.ps1"},
		},
		StagingDir: staging,
		BackupDir:  backup,
	}
	switch phase {
	case lifecycle.PhaseOldMoved:
		os.Rename(appDir, backup)
	case lifecycle.PhaseNewPlaced:
		os.Rename(appDir, backup)
		os.Rename(staging, appDir)
	}
	data, _ := json.Marshal(journal)
	if err := os.WriteFile(filepath.Join(l.root, "transactions", "current.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	l.imeCalls = nil
	return l
}

func TestRecoverStandardMatchesIMEToAppDir(t *testing.T) {
	tests := []struct {
		name  string
		phase string
		want  string
	}{
		{name: "元へ戻したら、DLL も元のバージョンに戻す", phase: lifecycle.PhaseOldMoved, want: "exe v1.3.0"},
		{name: "引き継ぎを終えたら、DLL を新しいバージョンにそろえる", phase: lifecycle.PhaseNewPlaced, want: "exe v1.4.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := interruptedStandardUse(t, tt.phase)

			got := l.run(t, false, "", "recover", "-y")

			if got.code != ExitOK || readFile(filepath.Join(l.standardAppDir(), "Meltype.exe")) != tt.want {
				t.Fatalf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
			}
			if len(l.imeCalls) != 1 || l.imeCalls[0].appDir != l.standardAppDir() || l.imeCalls[0].deployed != tt.want {
				t.Errorf("DLL を実行先のバージョンに合わせていない: %+v", l.imeCalls)
			}
		})
	}
	t.Run("DLL を合わせられなければ、実行先を戻したうえで知らせる", func(t *testing.T) {
		l := interruptedStandardUse(t, lifecycle.PhaseOldMoved)
		l.imeResult = winime.ResultCanceled

		got := l.run(t, false, "", "recover", "-y")

		if got.code == ExitOK || !strings.Contains(got.stderr, "管理者権限") {
			t.Errorf("code = %d\n%s", got.code, got.stderr)
		}
		if readFile(filepath.Join(l.standardAppDir(), "Meltype.exe")) != "exe v1.3.0" {
			t.Errorf("実行先を戻していない")
		}
		if next := l.run(t, false, "", "recover", "-y"); !strings.Contains(next.stdout, "中断した変更はありません") {
			t.Errorf("処理記録が残っている: %s", next.stdout)
		}
	})
	t.Run("portable 版の復旧では DLL に触れない", func(t *testing.T) {
		l := interruptedUse(t, lifecycle.PhaseOldMoved)
		l.imeRegistered = true

		l.run(t, false, "", "recover", "-y")

		if len(l.imeCalls) != 0 {
			t.Errorf("portable 版なのに DLL を入れ替えた: %+v", l.imeCalls)
		}
	})
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
