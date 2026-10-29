package remote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otnc/vmeltype/internal/catalog"
)

var now = time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

func newClient(server *httptest.Server) *Client {
	return &Client{
		HTTP:      server.Client(),
		BaseURL:   server.URL,
		UserAgent: "vmeltype/test",
		Now:       func() time.Time { return now },
		Sleep:     func(context.Context, time.Duration) error { return nil },
	}
}

const page1 = `[
  {"id": 3, "tag_name": "v1.4.0", "name": "Meltype 1.4.0", "draft": false, "prerelease": false,
   "published_at": "2026-10-28T03:31:20Z", "html_url": "https://github.com/yksr-melt/Meltype/releases/tag/v1.4.0",
   "assets": [{"id": 30, "name": "Meltype-1.4.0-windows.zip", "size": 45717254, "content_type": "application/zip",
     "digest": "sha256:e2ad", "browser_download_url": "https://github.com/yksr-melt/Meltype/releases/download/v1.4.0/Meltype-1.4.0-windows.zip"}]},
  {"id": 99, "tag_name": "v9.9.9", "draft": true, "prerelease": false, "assets": []}
]`

const page2 = `[
  {"id": 2, "tag_name": "v1.3.1-pre.3", "draft": false, "prerelease": true, "published_at": "2026-10-25T07:50:07Z", "assets": []}
]`

func TestFetchFollowsAllPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/yksr-melt/Meltype/releases" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, page2)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/yksr-melt/Meltype/releases?per_page=100&page=2>; rel="next", <http://%s/repos/yksr-melt/Meltype/releases?per_page=100&page=2>; rel="last"`, r.Host, r.Host))
		fmt.Fprint(w, page1)
	}))
	defer server.Close()

	got, err := newClient(server).Fetch(context.Background(), catalog.Repository, nil)
	if err != nil {
		t.Fatal(err)
	}

	releases := got.Releases()
	if len(releases) != 2 || releases[0].Tag != "v1.4.0" || releases[1].Tag != "v1.3.1-pre.3" {
		t.Fatalf("全ページの公開済みのリリースだけを取れていない: %+v", releases)
	}
	if !releases[1].Prerelease || releases[0].Prerelease {
		t.Errorf("prerelease フラグで分けていない: %+v", releases)
	}
	asset := releases[0].Assets[0]
	if asset.Digest != "sha256:e2ad" || !strings.HasSuffix(asset.DownloadURL, "/Meltype-1.4.0-windows.zip") || asset.Size != 45717254 {
		t.Errorf("配布物の情報が足りない: %+v", asset)
	}
	if !got.Complete || !got.FetchedAt.Equal(now) || !got.RevalidatedAt.Equal(now) || got.Repository != catalog.Repository {
		t.Errorf("取得の状態が正しくない: %+v", got)
	}
}

func TestFetchReusesUnchangedPagesByETag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		fmt.Fprint(w, page2)
	}))
	defer server.Close()
	client := newClient(server)
	first, err := client.Fetch(context.Background(), catalog.Repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	client.Now = func() time.Time { return later }

	second, err := client.Fetch(context.Background(), catalog.Repository, first)
	if err != nil {
		t.Fatal(err)
	}

	if len(second.Releases()) != 1 || second.Releases()[0].Tag != "v1.3.1-pre.3" {
		t.Errorf("304 のページで前の内容を使っていない: %+v", second.Releases())
	}
	if !second.FetchedAt.Equal(now) || !second.RevalidatedAt.Equal(later) {
		t.Errorf("FetchedAt = %v, RevalidatedAt = %v", second.FetchedAt, second.RevalidatedAt)
	}
}

func TestFetchRetriesServerErrors(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, page2)
	}))
	defer server.Close()

	got, err := newClient(server).Fetch(context.Background(), catalog.Repository, nil)

	if err != nil || len(got.Releases()) != 1 {
		t.Errorf("一時的な失敗のあとに取得できていない: %v", err)
	}
}

func TestFetchGivesUpAfterLimitedRetries(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := newClient(server).Fetch(context.Background(), catalog.Repository, nil)

	if err == nil {
		t.Fatal("失敗が続いても成功として返した")
	}
	if calls.Load() != maxAttempts {
		t.Errorf("%d 回試した (want %d)", calls.Load(), maxAttempts)
	}
}

func TestFetchFailsWhenALaterPageFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/yksr-melt/Meltype/releases?page=2>; rel="next"`, r.Host))
		fmt.Fprint(w, page1)
	}))
	defer server.Close()

	got, err := newClient(server).Fetch(context.Background(), catalog.Repository, nil)

	if err == nil || got != nil {
		t.Errorf("途中のページの失敗で部分的な一覧を返した: %+v, %v", got, err)
	}
}

func TestFetchReportsRateLimitResetTime(t *testing.T) {
	reset := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(reset.Unix()))
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	_, err := newClient(server).Fetch(context.Background(), catalog.Repository, nil)

	var rateLimit *RateLimitError
	if !errors.As(err, &rateLimit) || !rateLimit.ResetAt.Equal(reset) {
		t.Errorf("レート制限の解除時刻を返していない: %v", err)
	}
}

func TestFetchSendsTokenOnlyWhenConfigured(t *testing.T) {
	for _, token := range []string{"", "secret-token"} {
		t.Run(fmt.Sprintf("トークンの有無 %t", token != ""), func(t *testing.T) {
			var gotAuth string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				fmt.Fprint(w, "[]")
			}))
			defer server.Close()
			client := newClient(server)
			client.Token = token

			if _, err := client.Fetch(context.Background(), catalog.Repository, nil); err != nil {
				t.Fatal(err)
			}

			want := ""
			if token != "" {
				want = "Bearer " + token
			}
			if gotAuth != want {
				t.Errorf("Authorization = %q, want %q", gotAuth, want)
			}
		})
	}
}
