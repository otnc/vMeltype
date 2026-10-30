package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fakeReleases = `[
  {"id": 3, "tag_name": "v1.4.0", "draft": false, "prerelease": false, "published_at": "2026-10-28T03:31:20Z",
   "html_url": "https://github.com/yksr-melt/Meltype/releases/tag/v1.4.0",
   "assets": [{"id": 30, "name": "Meltype-1.4.0-windows.zip", "size": 1, "browser_download_url": "https://example.com/a.zip"}]},
  {"id": 2, "tag_name": "v1.3.1-pre.3", "draft": false, "prerelease": true, "published_at": "2026-10-25T07:50:07Z", "assets": []},
  {"id": 1, "tag_name": "v1.3.0", "draft": false, "prerelease": false, "published_at": "2026-10-20T00:00:00Z", "assets": []}
]`

type fakeGitHub struct {
	server *httptest.Server
	calls  atomic.Int32
	fail   atomic.Bool
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if f.fail.Load() {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, fakeReleases)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGitHub) execute(t *testing.T, root string, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := Env{
		In:           strings.NewReader(""),
		Out:          &stdout,
		Err:          &stderr,
		Version:      "1.2.3",
		Now:          func() time.Time { return time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC) },
		Getenv:       func(string) string { return "" },
		DefaultRoot:  func() (string, error) { return "", errors.New("テストでは --root を指定する") },
		HTTPClient:   f.server.Client(),
		GitHubAPIURL: f.server.URL,
	}
	code := Run(append([]string{"--root", root}, args...), env)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestFetchSavesCatalogForCachedReleases(t *testing.T) {
	github := newFakeGitHub(t)
	root := t.TempDir()

	fetched := github.execute(t, root, "fetch")
	callsAfterFetch := github.calls.Load()
	cached := github.execute(t, root, "releases", "--cached")

	if fetched.code != ExitOK {
		t.Fatalf("fetch: code = %d, stderr = %s", fetched.code, fetched.stderr)
	}
	if cached.code != ExitOK || !strings.Contains(cached.stdout, "v1.4.0") {
		t.Errorf("保存済みの一覧を表示していない: code = %d\n%s%s", cached.code, cached.stdout, cached.stderr)
	}
	if !strings.Contains(cached.stdout, "最新とは限りません") {
		t.Errorf("保存済みの一覧であることを示していない:\n%s", cached.stdout)
	}
	if github.calls.Load() != callsAfterFetch {
		t.Errorf("--cached で通信した")
	}
}

func TestReleasesHidesPrereleasesUnlessAll(t *testing.T) {
	github := newFakeGitHub(t)

	stable := github.execute(t, t.TempDir(), "releases")
	all := github.execute(t, t.TempDir(), "releases", "--all")

	if stable.code != ExitOK || strings.Contains(stable.stdout, "v1.3.1-pre.3") || !strings.Contains(stable.stdout, "v1.3.0") {
		t.Errorf("既定で正式版だけを表示していない:\n%s%s", stable.stdout, stable.stderr)
	}
	if !strings.Contains(stable.stdout, "--all") {
		t.Errorf("プレリリースを省いたことを示していない:\n%s", stable.stdout)
	}
	if all.code != ExitOK || !strings.Contains(all.stdout, "v1.3.1-pre.3") {
		t.Errorf("--all でプレリリースを表示していない:\n%s%s", all.stdout, all.stderr)
	}
}

func TestReleasesFetchesEveryTime(t *testing.T) {
	github := newFakeGitHub(t)
	root := t.TempDir()

	github.execute(t, root, "releases")
	github.execute(t, root, "releases")

	if github.calls.Load() != 2 {
		t.Errorf("releases のたびに取得していない: %d 回", github.calls.Load())
	}
}

func TestReleasesFailureDoesNotShowOrReplaceSavedCatalog(t *testing.T) {
	github := newFakeGitHub(t)
	root := t.TempDir()
	github.execute(t, root, "fetch")
	saved, err := os.ReadFile(filepath.Join(root, "catalog", "releases.json"))
	if err != nil {
		t.Fatal(err)
	}
	github.fail.Store(true)

	got := github.execute(t, root, "releases")

	if got.code != ExitError {
		t.Errorf("code = %d, want %d", got.code, ExitError)
	}
	if strings.Contains(got.stdout, "v1.4.0") {
		t.Errorf("取得に失敗したのに古い一覧を表示した:\n%s", got.stdout)
	}
	if !strings.Contains(got.stderr, "--cached") {
		t.Errorf("保存済みの一覧の見方を案内していない: %s", got.stderr)
	}
	after, _ := os.ReadFile(filepath.Join(root, "catalog", "releases.json"))
	if !bytes.Equal(saved, after) {
		t.Errorf("失敗した取得で保存済みの一覧が変わった")
	}
}

func TestReleasesOfflineRequiresCached(t *testing.T) {
	github := newFakeGitHub(t)

	got := github.execute(t, t.TempDir(), "releases", "--offline")

	if got.code != ExitUsage || !strings.Contains(got.stderr, "--cached") {
		t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
	}
	if github.calls.Load() != 0 {
		t.Errorf("--offline で通信した")
	}
}

func TestReleasesCachedWithoutSavedCatalog(t *testing.T) {
	github := newFakeGitHub(t)

	got := github.execute(t, t.TempDir(), "releases", "--cached")

	if got.code != ExitError || !strings.Contains(got.stderr, "meltool fetch") {
		t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
	}
}

func TestReleasesJSON(t *testing.T) {
	github := newFakeGitHub(t)

	got := github.execute(t, t.TempDir(), "releases", "--json")

	var body struct {
		Repository string `json:"repository"`
		Source     string `json:"source"`
		Releases   []struct {
			Tag        string `json:"tag"`
			Prerelease bool   `json:"prerelease"`
		} `json:"releases"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &body); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, got.stdout)
	}
	if body.Repository != "yksr-melt/Meltype" || body.Source != "remote" || len(body.Releases) != 2 {
		t.Errorf("JSON の内容が正しくない: %+v", body)
	}
}
