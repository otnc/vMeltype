package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// portableZip は、portable 版の配布物と同じ形の ZIP を作る。
func portableZip(t *testing.T, tag string) []byte {
	t.Helper()
	return makeZip(t, map[string]string{"Meltype.exe": "exe " + tag, "Meltype.portable": "portable", "dotnet/host.dll": "dll " + tag})
}

// updateEnv は、公式に v1.3.0、v1.4.0、v1.5.0-pre.1 があり、v1.3.0 の portable 版が有効な状態を作る。
func updateEnv(t *testing.T) *localEnv {
	t.Helper()
	l := newLocalEnv(t)
	var releases []fakeRelease
	for _, r := range []struct {
		tag string
		pre bool
	}{{"v1.3.0", false}, {"v1.4.0", false}, {"v1.5.0-pre.1", true}} {
		zip := portableZip(t, r.tag)
		releases = append(releases, fakeRelease{tag: r.tag, prerelease: r.pre, zip: zip, digest: digestOf(zip)})
	}
	l.upstream = newFakeUpstream(t, releases...)
	if got := l.run(t, false, "", "install", "1.3.0", "--portable", "-y"); got.code != ExitOK {
		t.Fatal(got.stderr)
	}
	if got := l.run(t, false, "", "use", "1.3.0", "--portable", "-y"); got.code != ExitOK {
		t.Fatal(got.stderr)
	}
	return l
}

func TestCheck(t *testing.T) {
	t.Run("新しい正式版があれば、終了コード 10 で示し、何も変えない", func(t *testing.T) {
		l := updateEnv(t)

		got := l.run(t, false, "", "check")

		if got.code != ExitUpdateAvailable || !strings.Contains(got.stdout, "v1.4.0") {
			t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
		}
		if l.upstream.downloads.Load() != 1 || l.read(t, "Meltype.exe") != "exe v1.3.0" {
			t.Errorf("check で取得か切り替えをした")
		}
	})
	t.Run("--pre ではプレリリースも含める", func(t *testing.T) {
		l := updateEnv(t)

		got := l.run(t, false, "", "check", "--pre", "--json")

		var body struct {
			Current, Latest string
			UpdateAvailable bool
		}
		json.Unmarshal([]byte(got.stdout), &body)
		if got.code != ExitUpdateAvailable || body.Latest != "v1.5.0-pre.1" || body.Current != "v1.3.0" || !body.UpdateAvailable {
			t.Errorf("code = %d, body = %+v\n%s", got.code, body, got.stderr)
		}
	})
	t.Run("最新なら終了コード 0", func(t *testing.T) {
		l := updateEnv(t)
		l.run(t, false, "", "update", "-y")

		got := l.run(t, false, "", "check")

		if got.code != ExitOK || !strings.Contains(got.stdout, "最新") {
			t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
		}
	})
	t.Run("有効なバージョンがなければ use を案内する", func(t *testing.T) {
		l := newLocalEnv(t)

		got := l.run(t, false, "", "check")

		if got.code != ExitError || !strings.Contains(got.stderr, "use") {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
}

func TestUpdate(t *testing.T) {
	t.Run("新しいバージョンを取得して切り替える", func(t *testing.T) {
		for _, name := range []string{"update", "up", "upgrade"} {
			l := updateEnv(t)

			got := l.run(t, false, "", name, "-y")

			if got.code != ExitOK || l.read(t, "Meltype.exe") != "exe v1.4.0" {
				t.Errorf("%s: code = %d, Meltype.exe = %q\n%s", name, got.code, l.read(t, "Meltype.exe"), got.stderr)
			}
		}
	})
	t.Run("--pre ではプレリリースに切り替える", func(t *testing.T) {
		l := updateEnv(t)

		got := l.run(t, false, "", "update", "--pre", "-y")

		if got.code != ExitOK || l.read(t, "Meltype.exe") != "exe v1.5.0-pre.1" {
			t.Errorf("code = %d, Meltype.exe = %q\n%s", got.code, l.read(t, "Meltype.exe"), got.stderr)
		}
	})
	t.Run("最新なら何もしない", func(t *testing.T) {
		l := updateEnv(t)
		l.run(t, false, "", "update", "-y")
		downloads := l.upstream.downloads.Load()

		got := l.run(t, false, "", "update", "-y")

		if got.code != ExitOK || !strings.Contains(got.stdout, "最新") || l.upstream.downloads.Load() != downloads {
			t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
		}
	})
	t.Run("固定していれば、取得する前に止める", func(t *testing.T) {
		l := updateEnv(t)
		l.run(t, false, "", "pin")

		got := l.run(t, false, "", "update", "-y")

		if got.code != ExitNeedsAction || !strings.Contains(got.stderr, "--override-pin") || l.upstream.downloads.Load() != 1 {
			t.Errorf("code = %d, downloads = %d\n%s", got.code, l.upstream.downloads.Load(), got.stderr)
		}
		if over := l.run(t, false, "", "update", "--override-pin", "-y"); over.code != ExitOK || l.read(t, "Meltype.exe") != "exe v1.4.0" {
			t.Errorf("--override-pin で更新できない: %d %s", over.code, over.stderr)
		}
	})
	t.Run("--dry-run は取得も切り替えもしない", func(t *testing.T) {
		l := updateEnv(t)

		got := l.run(t, false, "", "update", "--dry-run")

		if got.code != ExitOK || !strings.Contains(got.stdout, "v1.4.0") || l.upstream.downloads.Load() != 1 || l.read(t, "Meltype.exe") != "exe v1.3.0" {
			t.Errorf("code = %d, downloads = %d\n%s%s", got.code, l.upstream.downloads.Load(), got.stdout, got.stderr)
		}
	})
	t.Run("対話で取得を断れば何も変えない", func(t *testing.T) {
		l := updateEnv(t)

		got := l.run(t, true, "n\n", "update")

		if got.code != ExitCanceled || l.upstream.downloads.Load() != 1 {
			t.Errorf("code = %d, downloads = %d", got.code, l.upstream.downloads.Load())
		}
	})
}

func TestCheckSuggestsPreForPrerelease(t *testing.T) {
	l := updateEnv(t)
	l.run(t, false, "", "update", "--pre", "-y")

	for _, args := range [][]string{{"check"}, {"update", "-y"}} {
		got := l.run(t, false, "", args...)

		if got.code != ExitOK || !strings.Contains(got.stdout, "最新") || !strings.Contains(got.stdout, "--pre") {
			t.Errorf("%v: プレリリースを使っているのに --pre を案内していない: code = %d\n%s%s", args, got.code, got.stdout, got.stderr)
		}
	}
	stable := updateEnv(t)
	stable.run(t, false, "", "update", "-y")
	if got := stable.run(t, false, "", "check"); strings.Contains(got.stdout, "--pre") {
		t.Errorf("正式版を使っているのに --pre を案内した: %s", got.stdout)
	}
}
