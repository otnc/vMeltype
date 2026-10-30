package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otnc/meltool/internal/assets"
)

// selfRelease は、Meltool 自身のリリース。
type selfRelease struct {
	tag    string
	pre    bool
	zip    []byte
	digest string
}

// selfUpdateEnv は、Meltool のリリースを返す偽の GitHub と、置き換えられる meltool.exe を用意する。
type selfUpdateEnv struct {
	server  *httptest.Server
	exe     string
	version string
}

func newSelfUpdateEnv(t *testing.T, version string, releases ...selfRelease) *selfUpdateEnv {
	t.Helper()
	e := &selfUpdateEnv{exe: filepath.Join(t.TempDir(), "meltool.exe"), version: version}
	if err := os.WriteFile(e.exe, []byte("meltool "+version), 0o700); err != nil {
		t.Fatal(err)
	}
	e.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/otnc/Meltool/releases" {
			var body []map[string]any
			for i, rel := range releases {
				name := "Meltool-" + strings.TrimPrefix(rel.tag, "v") + "-windows.zip"
				body = append(body, map[string]any{
					"id": i + 1, "tag_name": rel.tag, "prerelease": rel.pre,
					"published_at": time.Date(2026, 10, 1+i, 0, 0, 0, 0, time.UTC),
					"assets": []map[string]any{{"id": 10 + i, "name": name, "size": len(rel.zip), "digest": rel.digest,
						"browser_download_url": e.server.URL + "/download/" + rel.tag}},
				})
			}
			json.NewEncoder(w).Encode(body)
			return
		}
		for _, rel := range releases {
			if r.URL.Path == "/download/"+rel.tag {
				w.Write(rel.zip)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(e.server.Close)
	return e
}

func (e *selfUpdateEnv) run(t *testing.T, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := t.TempDir()
	env := Env{
		In: strings.NewReader(""), Out: &stdout, Err: &stderr,
		Version: e.version, Now: time.Now, Getenv: func(string) string { return "" },
		DefaultRoot:  func() (string, error) { return root, nil },
		HTTPClient:   e.server.Client(),
		GitHubAPIURL: e.server.URL,
		Platform:     assets.Platform{OS: "windows", Arch: "amd64"},
		Executable:   func() (string, error) { return e.exe, nil },
	}
	return result{code: Run(args, env), stdout: stdout.String(), stderr: stderr.String()}
}

func selfZip(t *testing.T, tag string) selfRelease {
	t.Helper()
	zip := makeZip(t, map[string]string{"meltool.exe": "meltool " + strings.TrimPrefix(tag, "v"), "LICENSE.txt": "license"})
	return selfRelease{tag: tag, pre: strings.Contains(tag, "-"), zip: zip, digest: digestOf(zip)}
}

func TestAppUpdate(t *testing.T) {
	t.Run("正式版の最新に置き換える", func(t *testing.T) {
		for _, args := range [][]string{{"app", "update"}, {"self", "upgrade"}} {
			e := newSelfUpdateEnv(t, "0.1.0-pre.2", selfZip(t, "v0.1.0-pre.2"), selfZip(t, "v0.1.0"), selfZip(t, "v0.2.0-pre.1"))

			got := e.run(t, append(args, "-y")...)

			if got.code != ExitOK || readFile(e.exe) != "meltool 0.1.0" {
				t.Errorf("%v: code = %d, exe = %q\n%s", args, got.code, readFile(e.exe), got.stderr)
			}
		}
	})
	t.Run("--pre ではプレリリースも含める", func(t *testing.T) {
		e := newSelfUpdateEnv(t, "0.1.0", selfZip(t, "v0.1.0"), selfZip(t, "v0.2.0-pre.1"))

		got := e.run(t, "app", "update", "--pre", "-y")

		if got.code != ExitOK || readFile(e.exe) != "meltool 0.2.0-pre.1" {
			t.Errorf("code = %d, exe = %q\n%s", got.code, readFile(e.exe), got.stderr)
		}
	})
	t.Run("最新なら何もしない", func(t *testing.T) {
		e := newSelfUpdateEnv(t, "0.1.0-pre.2", selfZip(t, "v0.1.0-pre.2"))

		got := e.run(t, "app", "update", "-y")

		if got.code != ExitOK || !strings.Contains(got.stdout, "最新") || readFile(e.exe) != "meltool 0.1.0-pre.2" {
			t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
		}
	})
	t.Run("SHA-256 が合わなければ置き換えない", func(t *testing.T) {
		broken := selfZip(t, "v0.1.0")
		broken.digest = digestOf([]byte("別のもの"))
		e := newSelfUpdateEnv(t, "0.1.0-pre.2", broken)

		got := e.run(t, "app", "update", "-y")

		if got.code != ExitError || !strings.Contains(got.stderr, "SHA-256") || readFile(e.exe) != "meltool 0.1.0-pre.2" {
			t.Errorf("code = %d, exe = %q\n%s", got.code, readFile(e.exe), got.stderr)
		}
	})
	t.Run("期待値がなければ置き換えない", func(t *testing.T) {
		unverified := selfZip(t, "v0.1.0")
		unverified.digest = ""
		e := newSelfUpdateEnv(t, "0.1.0-pre.2", unverified)

		got := e.run(t, "app", "update", "-y")

		if got.code != ExitError || readFile(e.exe) != "meltool 0.1.0-pre.2" {
			t.Errorf("code = %d, exe = %q\n%s", got.code, readFile(e.exe), got.stderr)
		}
	})
	t.Run("--dry-run と、確認できないときは置き換えない", func(t *testing.T) {
		for _, args := range [][]string{{"app", "update", "--dry-run"}, {"app", "update"}} {
			e := newSelfUpdateEnv(t, "0.1.0-pre.2", selfZip(t, "v0.1.0"))

			got := e.run(t, args...)

			if readFile(e.exe) != "meltool 0.1.0-pre.2" || !strings.Contains(got.stdout+got.stderr, "v0.1.0") {
				t.Errorf("%v: code = %d, exe = %q\n%s%s", args, got.code, readFile(e.exe), got.stdout, got.stderr)
			}
		}
	})
	t.Run("開発中のビルドは更新しない", func(t *testing.T) {
		e := newSelfUpdateEnv(t, "dev", selfZip(t, "v0.1.0"))

		got := e.run(t, "app", "update", "-y")

		if got.code != ExitError || readFile(e.exe) != "meltool dev" {
			t.Errorf("code = %d\n%s", got.code, got.stderr)
		}
	})
}
