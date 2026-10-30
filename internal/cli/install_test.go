package cli

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otnc/vmeltype/internal/assets"
	"github.com/otnc/vmeltype/internal/store"
)

type fakeRelease struct {
	tag        string
	prerelease bool
	zip        []byte
	// digest は、API が返す期待値。空なら期待値のない配布物になる
	digest string
}

type fakeUpstream struct {
	server    *httptest.Server
	releases  []fakeRelease
	apiCalls  atomic.Int32
	downloads atomic.Int32
	authSeen  atomic.Bool
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(body))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newFakeUpstream(t *testing.T, releases ...fakeRelease) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{releases: releases}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" && strings.HasPrefix(r.URL.Path, "/download/") {
			f.authSeen.Store(true)
		}
		switch {
		case r.URL.Path == "/repos/yksr-melt/Meltype/releases":
			f.apiCalls.Add(1)
			var body []map[string]any
			for i, rel := range f.releases {
				var assetList []map[string]any
				for j, platform := range []string{"windows", "windows-portable"} {
					name := fmt.Sprintf("Meltype-%s-%s.zip", strings.TrimPrefix(rel.tag, "v"), platform)
					assetList = append(assetList, map[string]any{
						"id": 100 + i*10 + j, "name": name, "size": len(rel.zip), "digest": rel.digest,
						"browser_download_url": f.server.URL + "/download/" + rel.tag + "/" + name,
					})
				}
				body = append(body, map[string]any{
					"id": i + 1, "tag_name": rel.tag, "prerelease": rel.prerelease,
					"published_at": time.Date(2026, 10, 1+i, 0, 0, 0, 0, time.UTC),
					"assets":       assetList,
				})
			}
			json.NewEncoder(w).Encode(body)
		case strings.HasPrefix(r.URL.Path, "/download/"):
			f.downloads.Add(1)
			for _, rel := range f.releases {
				if strings.HasPrefix(r.URL.Path, "/download/"+rel.tag+"/") {
					w.Write(rel.zip)
					return
				}
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeUpstream) run(t *testing.T, root string, interactive bool, stdin string, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := Env{
		In:           strings.NewReader(stdin),
		Out:          &stdout,
		Err:          &stderr,
		Interactive:  interactive,
		Version:      "1.2.3",
		Now:          func() time.Time { return time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC) },
		Getenv:       func(key string) string { return map[string]string{"VMELTYPE_GITHUB_TOKEN": "secret"}[key] },
		DefaultRoot:  func() (string, error) { return root, nil },
		HTTPClient:   f.server.Client(),
		GitHubAPIURL: f.server.URL,
		Platform:     assets.Platform{OS: "windows", Arch: "amd64"},
	}
	code := Run(args, env)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func storedTags(t *testing.T, root string) []string {
	t.Helper()
	stored, _, err := store.Store{Root: root}.List()
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, s := range stored {
		tags = append(tags, s.Tag)
	}
	return tags
}

func leftovers(t *testing.T, root string) []string {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(root, "versions"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestInstallStoresVerifiedVersion(t *testing.T) {
	zipData := makeZip(t, map[string]string{"Meltype/Meltype.exe": "binary"})
	upstream := newFakeUpstream(t, fakeRelease{tag: "v1.4.0", zip: zipData, digest: digestOf(zipData)})
	root := t.TempDir()

	got := upstream.run(t, root, false, "", "install", "1.4.0", "-y")

	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr = %s", got.code, got.stderr)
	}
	s, err := store.Store{Root: root}.Get(store.ID("v1.4.0", "windows", "amd64", "standard"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Archive.SHA256 != strings.TrimPrefix(digestOf(zipData), "sha256:") || s.Archive.ExpectedSource != store.ExpectedFromGitHubDigest {
		t.Errorf("アーカイブの検査値を記録していない: %+v", s.Archive)
	}
	body, err := os.ReadFile(filepath.Join(store.Store{Root: root}.Path(s.ID), "files", "Meltype", "Meltype.exe"))
	if err != nil || string(body) != "binary" {
		t.Errorf("展開物がない: %q, %v", body, err)
	}
	if upstream.authSeen.Load() {
		t.Errorf("配布物の取得に認証情報を送った")
	}
}

func TestInstallOfStoredVersionDoesNotUseNetwork(t *testing.T) {
	zipData := makeZip(t, map[string]string{"a": "b"})
	upstream := newFakeUpstream(t, fakeRelease{tag: "v1.4.0", zip: zipData, digest: digestOf(zipData)})
	root := t.TempDir()
	upstream.run(t, root, false, "", "install", "v1.4.0", "-y")
	callsBefore := upstream.apiCalls.Load() + upstream.downloads.Load()

	got := upstream.run(t, root, false, "", "i", "v1.4.0")

	if got.code != ExitOK || !strings.Contains(got.stdout, "既に保持") {
		t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	if upstream.apiCalls.Load()+upstream.downloads.Load() != callsBefore {
		t.Errorf("保持済みのバージョンの install で通信した")
	}
}

func TestInstallReinstallDownloadsAgain(t *testing.T) {
	zipData := makeZip(t, map[string]string{"a": "b"})
	upstream := newFakeUpstream(t, fakeRelease{tag: "v1.4.0", zip: zipData, digest: digestOf(zipData)})
	root := t.TempDir()
	upstream.run(t, root, false, "", "install", "v1.4.0", "-y")

	got := upstream.run(t, root, false, "", "install", "v1.4.0", "--reinstall", "-y")

	if got.code != ExitOK || upstream.downloads.Load() != 2 {
		t.Errorf("取得し直していない: code = %d, downloads = %d\n%s", got.code, upstream.downloads.Load(), got.stderr)
	}
}

func TestInstallRejectsDigestMismatch(t *testing.T) {
	zipData := makeZip(t, map[string]string{"a": "b"})
	upstream := newFakeUpstream(t, fakeRelease{tag: "v1.4.0", zip: zipData, digest: digestOf([]byte("different"))})
	root := t.TempDir()

	got := upstream.run(t, root, false, "", "install", "v1.4.0", "-y")

	if got.code != ExitError || !strings.Contains(got.stderr, "一致しません") {
		t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
	}
	if tags := storedTags(t, root); len(tags) != 0 {
		t.Errorf("一致しない配布物を保持した: %v", tags)
	}
	if left := leftovers(t, root); len(left) != 0 {
		t.Errorf("作業用のディレクトリが残っている: %v", left)
	}
}

func TestInstallRequiresExplicitPermissionForUnverifiedAsset(t *testing.T) {
	zipData := makeZip(t, map[string]string{"a": "b"})
	upstream := newFakeUpstream(t, fakeRelease{tag: "v1.4.0", zip: zipData})
	root := t.TempDir()

	refused := upstream.run(t, root, false, "", "install", "v1.4.0", "-y")
	allowed := upstream.run(t, root, false, "", "install", "v1.4.0", "-y", "--allow-unverified")

	if refused.code != ExitError || !strings.Contains(refused.stderr, "--allow-unverified") {
		t.Errorf("期待値のない配布物を -y だけで保持しようとした: code = %d, stderr = %s", refused.code, refused.stderr)
	}
	if allowed.code != ExitOK {
		t.Fatalf("--allow-unverified で保持できない: %s", allowed.stderr)
	}
	s, _ := store.Store{Root: root}.Get(store.ID("v1.4.0", "windows", "amd64", "standard"))
	if s.Archive.Expected != "" || s.Archive.ExpectedSource != "" {
		t.Errorf("未検証であることが manifest から分からない: %+v", s.Archive)
	}
}

func TestInstallLatest(t *testing.T) {
	stable := makeZip(t, map[string]string{"a": "stable"})
	pre := makeZip(t, map[string]string{"a": "pre"})
	upstream := newFakeUpstream(t,
		fakeRelease{tag: "v1.4.0", zip: stable, digest: digestOf(stable)},
		fakeRelease{tag: "v1.5.0-pre.1", prerelease: true, zip: pre, digest: digestOf(pre)},
	)

	t.Run("latest は正式版の最大", func(t *testing.T) {
		root := t.TempDir()
		got := upstream.run(t, root, false, "", "install", "latest", "-y")
		if tags := storedTags(t, root); got.code != ExitOK || len(tags) != 1 || tags[0] != "v1.4.0" {
			t.Errorf("code = %d, tags = %v, stderr = %s", got.code, tags, got.stderr)
		}
	})
	t.Run("latest --pre はプレリリースも含めた最大", func(t *testing.T) {
		root := t.TempDir()
		got := upstream.run(t, root, false, "", "install", "latest", "--pre", "-y")
		if tags := storedTags(t, root); got.code != ExitOK || len(tags) != 1 || tags[0] != "v1.5.0-pre.1" {
			t.Errorf("code = %d, tags = %v, stderr = %s", got.code, tags, got.stderr)
		}
	})
}

func TestInstallConfirmation(t *testing.T) {
	zipData := makeZip(t, map[string]string{"a": "b"})

	tests := []struct {
		name        string
		interactive bool
		stdin       string
		args        []string
		wantCode    int
		wantStored  bool
	}{
		{name: "対話で Enter なら取得する", interactive: true, stdin: "\n", args: []string{"install", "v1.4.0"}, wantCode: ExitOK, wantStored: true},
		{name: "対話で n なら取得しない", interactive: true, stdin: "n\n", args: []string{"install", "v1.4.0"}, wantCode: ExitCanceled},
		{name: "非 TTY で -y がなければ要操作で止まる", args: []string{"install", "v1.4.0"}, wantCode: ExitNeedsAction},
		{name: "--json では対話せず、-y がなければ止まる", interactive: true, stdin: "y\n", args: []string{"install", "v1.4.0", "--json"}, wantCode: ExitNeedsAction},
		{name: "--dry-run は計画だけを表示する", args: []string{"install", "v1.4.0", "--dry-run"}, wantCode: ExitOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := newFakeUpstream(t, fakeRelease{tag: "v1.4.0", zip: zipData, digest: digestOf(zipData)})
			root := t.TempDir()

			got := upstream.run(t, root, tt.interactive, tt.stdin, tt.args...)

			if got.code != tt.wantCode {
				t.Errorf("code = %d, want %d (stderr: %s)", got.code, tt.wantCode, got.stderr)
			}
			if stored := len(storedTags(t, root)) == 1; stored != tt.wantStored {
				t.Errorf("保持した = %t, want %t", stored, tt.wantStored)
			}
			if !tt.wantStored && upstream.downloads.Load() != 0 {
				t.Errorf("確認の前に配布物を取得した")
			}
			if !strings.Contains(got.stdout+got.stderr, "Meltype-1.4.0-windows.zip") {
				t.Errorf("計画に配布物を示していない:\n%s%s", got.stdout, got.stderr)
			}
		})
	}
}

func TestInstallOfflineDoesNotFetch(t *testing.T) {
	upstream := newFakeUpstream(t)

	got := upstream.run(t, t.TempDir(), false, "", "install", "v1.4.0", "--offline", "-y")

	if got.code != ExitError || upstream.apiCalls.Load() != 0 {
		t.Errorf("code = %d, apiCalls = %d, stderr = %s", got.code, upstream.apiCalls.Load(), got.stderr)
	}
}

func TestInstallRejectsUnsafeArchiveWithoutLeavingFiles(t *testing.T) {
	zipData := makeZip(t, map[string]string{"../evil": "x"})
	upstream := newFakeUpstream(t, fakeRelease{tag: "v1.4.0", zip: zipData, digest: digestOf(zipData)})
	root := t.TempDir()

	got := upstream.run(t, root, false, "", "install", "v1.4.0", "-y")

	if got.code != ExitError {
		t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
	}
	if left := leftovers(t, root); len(left) != 0 {
		t.Errorf("作業用のディレクトリが残っている: %v", left)
	}
}

func TestInstallUnknownTag(t *testing.T) {
	zipData := makeZip(t, map[string]string{"a": "b"})
	upstream := newFakeUpstream(t, fakeRelease{tag: "v1.4.0", zip: zipData, digest: digestOf(zipData)})

	got := upstream.run(t, t.TempDir(), false, "", "install", "v9.9.9", "-y")

	if got.code != ExitError || !strings.Contains(got.stderr, "9.9.9") {
		t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
	}
}

func TestInstallSelectsVariantByPortableFlag(t *testing.T) {
	zipData := makeZip(t, map[string]string{"a": "b"})
	upstream := newFakeUpstream(t, fakeRelease{tag: "v1.4.0", zip: zipData, digest: digestOf(zipData)})
	root := t.TempDir()

	standard := upstream.run(t, root, false, "", "install", "v1.4.0", "-y")
	portable := upstream.run(t, root, false, "", "install", "v1.4.0", "--portable", "-y")

	if standard.code != ExitOK || portable.code != ExitOK {
		t.Fatalf("code = %d, %d\n%s%s", standard.code, portable.code, standard.stderr, portable.stderr)
	}
	ids := storedIDs(t, root)
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Errorf("通常版と portable 版を別に保持していない: %v", ids)
	}
	if !strings.Contains(portable.stdout, "Meltype-1.4.0-windows-portable.zip") {
		t.Errorf("--portable で portable 版の配布物を選んでいない:\n%s", portable.stdout)
	}
}

func TestInstallVersionQuery(t *testing.T) {
	var releases []fakeRelease
	for _, rel := range []struct {
		tag string
		pre bool
	}{{"v1.3.0", false}, {"v1.4.0", false}, {"v1.4.1-pre.1", true}, {"v1.4.1-pre.2", true}} {
		data := makeZip(t, map[string]string{"a": rel.tag})
		releases = append(releases, fakeRelease{tag: rel.tag, prerelease: rel.pre, zip: data, digest: digestOf(data)})
	}
	upstream := newFakeUpstream(t, releases...)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "バージョンを省くと正式版の最新", args: []string{"install"}, want: "v1.4.0"},
		{name: "v を省いたバージョン", args: []string{"install", "1.3.0"}, want: "v1.3.0"},
		{name: "範囲の指定で、その中の正式版の最新", args: []string{"install", "1.3"}, want: "v1.3.0"},
		{name: "メジャーだけの指定", args: []string{"install", "1"}, want: "v1.4.0"},
		{name: "--pre でプレリリースの最新", args: []string{"install", "--pre"}, want: "v1.4.1-pre.2"},
		{name: "バージョンと --pre で、そのバージョンのプレリリースの最新", args: []string{"install", "1.4.1", "--pre"}, want: "v1.4.1-pre.2"},
		{name: "明示したプレリリース", args: []string{"install", "1.4.1-pre.1"}, want: "v1.4.1-pre.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()

			got := upstream.run(t, root, false, "", append(tt.args, "-y")...)

			if tags := storedTags(t, root); got.code != ExitOK || len(tags) != 1 || tags[0] != tt.want {
				t.Errorf("code = %d, tags = %v, want %s\n%s", got.code, tags, tt.want, got.stderr)
			}
		})
	}

	t.Run("合うバージョンがなければ、指定の意味を示して止める", func(t *testing.T) {
		got := upstream.run(t, t.TempDir(), false, "", "install", "2", "-y")
		if got.code != ExitError || !strings.Contains(got.stderr, "2.x.x の正式版の最新") {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
	t.Run("解釈できない指定は引数の誤り", func(t *testing.T) {
		got := upstream.run(t, t.TempDir(), false, "", "install", "1.x", "-y")
		if got.code != ExitUsage {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
}
