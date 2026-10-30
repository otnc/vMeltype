// Package remote は、GitHub Releases API から本家の公式一覧を取得する。
// ローカル操作の経路から呼ばないよう、通信はこのパッケージに閉じ込める。
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/otnc/meltool/internal/catalog"
)

// DefaultBaseURL は、GitHub の REST API の URL。
const DefaultBaseURL = "https://api.github.com"

const (
	maxAttempts = 3
	maxPages    = 100
	// 1 ページは 100 件の情報だけなので、これを超える応答は異常とみなす
	maxResponseBytes = 32 << 20
	// Retry-After がこれより長いときは待たずに失敗を返し、ローカル操作の利用者を長く止めない
	maxRetryAfterWait = 10 * time.Second
)

// Client は、公式一覧を取得する。Token が空なら匿名で取得する。
type Client struct {
	HTTP      *http.Client
	BaseURL   string
	Token     string
	UserAgent string
	Now       func() time.Time
	Sleep     func(ctx context.Context, d time.Duration) error
}

// RateLimitError は、API の利用回数の上限に達したことを表す。
type RateLimitError struct {
	ResetAt time.Time
}

func (e *RateLimitError) Error() string {
	if e.ResetAt.IsZero() {
		return "GitHub の API の利用回数の上限に達しました。しばらくしてから試すか、MELTOOL_GITHUB_TOKEN を設定してください"
	}
	return fmt.Sprintf("GitHub の API の利用回数の上限に達しました。%s 以降に試すか、MELTOOL_GITHUB_TOKEN を設定してください", e.ResetAt.Local().Format("2006-01-02 15:04"))
}

// Fetch は、全ページを取得した公式一覧を返す。previous があれば、ページごとの ETag で変わっていないページを省く。
// どれかのページで失敗したら、部分的な一覧は返さずにエラーを返す。
//
//	c, err := client.Fetch(ctx, catalog.Repository, saved)
func (c *Client) Fetch(ctx context.Context, repository string, previous *catalog.Catalog) (*catalog.Catalog, error) {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Sleep == nil {
		c.Sleep = sleep
	}
	previousPages := map[string]catalog.Page{}
	if previous != nil && previous.Repository == repository {
		for _, p := range previous.Pages {
			previousPages[p.URL] = p
		}
	}

	next := fmt.Sprintf("%s/repos/%s/releases?per_page=100", c.BaseURL, repository)
	var pages []catalog.Page
	changed := previous == nil || previous.Repository != repository
	for next != "" {
		if len(pages) >= maxPages {
			return nil, fmt.Errorf("公式一覧のページが %d を超えたので、取得を止めました", maxPages)
		}
		cached, hasCached := previousPages[next]
		page, nextURL, notModified, err := c.fetchPage(ctx, next, cached.ETag)
		if err != nil {
			return nil, err
		}
		if notModified && hasCached {
			page = cached
		} else {
			changed = true
		}
		pages = append(pages, page)
		next = nextURL
	}
	if previous != nil && len(pages) != len(previous.Pages) {
		changed = true
	}

	now := c.Now().UTC()
	fetchedAt := now
	if !changed {
		fetchedAt = previous.FetchedAt
	}
	return &catalog.Catalog{
		Repository:    repository,
		FetchedAt:     fetchedAt,
		RevalidatedAt: now,
		Complete:      true,
		Pages:         pages,
	}, nil
}

type apiRelease struct {
	ID          int64      `json:"id"`
	TagName     string     `json:"tag_name"`
	Name        string     `json:"name"`
	Draft       bool       `json:"draft"`
	Prerelease  bool       `json:"prerelease"`
	PublishedAt time.Time  `json:"published_at"`
	HTMLURL     string     `json:"html_url"`
	Assets      []apiAsset `json:"assets"`
}

type apiAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
	Digest             string `json:"digest"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func (c *Client) fetchPage(ctx context.Context, pageURL, etag string) (page catalog.Page, next string, notModified bool, err error) {
	resp, err := c.get(ctx, pageURL, etag)
	if err != nil {
		return catalog.Page{}, "", false, err
	}
	defer resp.Body.Close()

	next = nextLink(resp.Header.Get("Link"))
	if next != "" {
		if err := sameOrigin(pageURL, next); err != nil {
			return catalog.Page{}, "", false, err
		}
	}
	if resp.StatusCode == http.StatusNotModified {
		return catalog.Page{URL: pageURL, ETag: etag}, next, true, nil
	}

	var raw []apiRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&raw); err != nil {
		return catalog.Page{}, "", false, fmt.Errorf("公式一覧の応答を読めませんでした: %w", err)
	}
	page = catalog.Page{URL: pageURL, ETag: resp.Header.Get("ETag"), Releases: []catalog.Release{}}
	for _, r := range raw {
		if r.Draft {
			continue
		}
		release := catalog.Release{
			ID:          r.ID,
			Tag:         r.TagName,
			Name:        r.Name,
			Prerelease:  r.Prerelease,
			PublishedAt: r.PublishedAt,
			URL:         r.HTMLURL,
			Assets:      []catalog.Asset{},
		}
		for _, a := range r.Assets {
			release.Assets = append(release.Assets, catalog.Asset{
				ID:          a.ID,
				Name:        a.Name,
				Size:        a.Size,
				ContentType: a.ContentType,
				Digest:      a.Digest,
				DownloadURL: a.BrowserDownloadURL,
			})
		}
		page.Releases = append(page.Releases, release)
	}
	return page, next, false, nil
}

// get は、200 か 304 の応答を返す。一時的な失敗は上限まで試し直す。
func (c *Client) get(ctx context.Context, pageURL, etag string) (*http.Response, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if err := c.Sleep(ctx, time.Duration(attempt-1)*time.Second); err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", c.UserAgent)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}

		resp, err := c.HTTP.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("GitHub に接続できませんでした: %w", err)
			continue
		}
		switch {
		case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotModified:
			return resp, nil
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
			resp.Body.Close()
			wait, limited := rateLimitWait(resp.Header, c.Now())
			if !limited {
				return nil, fmt.Errorf("GitHub が公式一覧の取得を拒否しました (%s)", resp.Status)
			}
			if wait <= 0 || wait > maxRetryAfterWait || attempt == maxAttempts {
				return nil, &RateLimitError{ResetAt: rateLimitReset(resp.Header)}
			}
			if err := c.Sleep(ctx, wait); err != nil {
				return nil, err
			}
			lastErr = &RateLimitError{}
		case resp.StatusCode >= 500:
			resp.Body.Close()
			lastErr = fmt.Errorf("GitHub が一時的に応答できませんでした (%s)", resp.Status)
		default:
			resp.Body.Close()
			return nil, fmt.Errorf("公式一覧を取得できませんでした (%s)", resp.Status)
		}
	}
	return nil, lastErr
}

// rateLimitWait は、レート制限による拒否かと、待てば解除される時間を返す。
func rateLimitWait(h http.Header, now time.Time) (time.Duration, bool) {
	if s := h.Get("Retry-After"); s != "" {
		if seconds, err := strconv.Atoi(s); err == nil {
			return time.Duration(seconds) * time.Second, true
		}
		return 0, true
	}
	if h.Get("X-RateLimit-Remaining") == "0" {
		if reset := rateLimitReset(h); !reset.IsZero() {
			return reset.Sub(now), true
		}
		return 0, true
	}
	return 0, false
}

func rateLimitReset(h http.Header) time.Time {
	seconds, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

var nextLinkPattern = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func nextLink(header string) string {
	if m := nextLinkPattern.FindStringSubmatch(header); m != nil {
		return m[1]
	}
	return ""
}

// sameOrigin は、次のページの URL が最初の API と同じ場所かを確かめ、トークンを別のホストへ送らないようにする。
func sameOrigin(current, next string) error {
	a, errA := url.Parse(current)
	b, errB := url.Parse(next)
	if errA != nil || errB != nil || a.Scheme != b.Scheme || a.Host != b.Host {
		return errors.New("公式一覧の次のページが、別の場所を指していたので取得を止めました")
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ErrTooLarge は、配布物が公式一覧に書かれたサイズより大きかったことを表す。
var ErrTooLarge = errors.New("配布物が公式一覧のサイズより大きいので、取得を止めました")

// Download は、配布物を w へ書き、書いたバイト数を返す。配布物は匿名で取れるので、認証情報は送らない。
// 書きかけの w へは試し直せないので、失敗したら呼び出し側が作業用のファイルごと捨てる。
func (c *Client) Download(ctx context.Context, downloadURL string, maxBytes int64, w io.Writer) (int64, error) {
	if err := c.checkDownloadURL(downloadURL); err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("配布物を取得できませんでした: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("配布物を取得できませんでした (%s)", resp.Status)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		return n, fmt.Errorf("配布物の取得が途中で失敗しました: %w", err)
	}
	if n > maxBytes {
		return n, ErrTooLarge
	}
	return n, nil
}

// checkDownloadURL は、配布物の URL が HTTPS か、API と同じ場所 (試験用のサーバー) であることを確かめる。
func (c *Client) checkDownloadURL(downloadURL string) error {
	u, err := url.Parse(downloadURL)
	if err != nil {
		return fmt.Errorf("配布物の URL を読めません: %w", err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if base, err := url.Parse(c.BaseURL); err == nil && u.Scheme == base.Scheme && u.Host == base.Host {
		return nil
	}
	return fmt.Errorf("配布物の URL が HTTPS ではないので取得しません: %s", downloadURL)
}
