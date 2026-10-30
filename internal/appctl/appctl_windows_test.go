//go:build windows

package appctl

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/otnc/meltool/internal/osproc"
)

var (
	fakeOnce sync.Once
	fakeExe  string
	fakeErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if fakeExe != "" {
		os.RemoveAll(filepath.Dir(fakeExe))
	}
	os.Exit(code)
}

// placeFake は、--exit を真似る偽の Meltype.exe を、新しい実行先に置く。
func placeFake(t *testing.T) string {
	t.Helper()
	fakeOnce.Do(func() {
		dir, err := os.MkdirTemp("", "meltool-fake-")
		if err != nil {
			fakeErr = err
			return
		}
		fakeExe = filepath.Join(dir, ExeName)
		out, err := exec.Command("go", "build", "-o", fakeExe, "./testdata/fakemeltype").CombinedOutput()
		if err != nil {
			fakeErr = errors.New(string(out))
		}
	})
	if fakeErr != nil {
		t.Fatalf("偽の Meltype をビルドできない: %v", fakeErr)
	}
	appDir := t.TempDir()
	data, err := os.ReadFile(fakeExe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, ExeName), data, 0o700); err != nil {
		t.Fatal(err)
	}
	return appDir
}

// startFake は、偽の Meltype を起動し、動き出すまで待つ。テストの終わりに必ず止める。
func startFake(t *testing.T, appDir string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(appDir, ExeName))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	waitRunning(t, appDir)
}

func waitRunning(t *testing.T, appDir string) {
	t.Helper()
	for range 100 {
		if running, _ := osproc.RunningUnder(appDir); len(running) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("偽の Meltype が動き出さない")
}

func TestStop(t *testing.T) {
	t.Run("終了の合図を送り、終わるまで待つ", func(t *testing.T) {
		appDir := placeFake(t)
		startFake(t, appDir)

		err := Stop(context.Background(), appDir, osproc.RunningUnder, 10*time.Second)

		if err != nil {
			t.Fatal(err)
		}
		if running, _ := osproc.RunningUnder(appDir); len(running) != 0 {
			t.Errorf("まだ動いている: %v", running)
		}
	})
	t.Run("起動した直後で合図を受け取れなければ、送り直す", func(t *testing.T) {
		appDir := placeFake(t)
		os.WriteFile(filepath.Join(appDir, "slow-start"), nil, 0o600)
		startFake(t, appDir)

		err := Stop(context.Background(), appDir, osproc.RunningUnder, 10*time.Second)

		if err != nil {
			t.Fatal(err)
		}
		if running, _ := osproc.RunningUnder(appDir); len(running) != 0 {
			t.Errorf("まだ動いている: %v", running)
		}
	})
	t.Run("終わらなければ、強制終了せずにあきらめる", func(t *testing.T) {
		appDir := placeFake(t)
		os.WriteFile(filepath.Join(appDir, "ignore-exit"), nil, 0o600)
		startFake(t, appDir)

		err := Stop(context.Background(), appDir, osproc.RunningUnder, time.Second)

		if !errors.Is(err, ErrStillRunning) {
			t.Errorf("err = %v", err)
		}
		if running, _ := osproc.RunningUnder(appDir); len(running) == 0 {
			t.Errorf("強制終了した")
		}
	})
}

func TestStart(t *testing.T) {
	appDir := placeFake(t)

	if err := Start(appDir); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		os.WriteFile(filepath.Join(appDir, "exit-requested"), nil, 0o600)
		for range 100 {
			if running, _ := osproc.RunningUnder(appDir); len(running) == 0 {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	waitRunning(t, appDir)
}
