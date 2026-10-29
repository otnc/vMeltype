package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otnc/vmeltype/internal/store"
)

// executeOffline は、通信の手段を渡さずにコマンドを実行する。通信しようとすれば nil の HTTP クライアントで失敗する。
func executeOffline(t *testing.T, root string, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := Env{
		In:          strings.NewReader(""),
		Out:         &stdout,
		Err:         &stderr,
		Version:     "1.2.3",
		Now:         func() time.Time { return time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC) },
		Getenv:      func(string) string { return "" },
		DefaultRoot: func() (string, error) { return root, nil },
	}
	code := Run(args, env)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func storeVersion(t *testing.T, root, tag string) {
	t.Helper()
	s := store.Store{Root: root}
	staging, err := s.Stage()
	if err != nil {
		t.Fatal(err)
	}
	err = staging.Commit(store.Stored{
		ID:       store.ID(tag, "windows", "amd64", "standard"),
		Tag:      tag,
		OS:       "windows",
		Arch:     "amd64",
		Variant:  "standard",
		StoredAt: time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestListShowsStoredVersions(t *testing.T) {
	root := t.TempDir()
	storeVersion(t, root, "v1.4.0")
	storeVersion(t, root, "v1.3.0")

	got := executeOffline(t, root, "list")

	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr = %s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "v1.4.0_windows-amd64_standard") || !strings.Contains(got.stdout, "v1.3.0") {
		t.Errorf("保持版を表示していない:\n%s", got.stdout)
	}
}

func TestListWithoutStoredVersions(t *testing.T) {
	got := executeOffline(t, t.TempDir(), "ls")

	if got.code != ExitOK || !strings.Contains(got.stdout, "vmeltype install") {
		t.Errorf("保持版がないときの案内がない: code = %d\n%s", got.code, got.stdout)
	}
}

func TestListReportsBrokenVersions(t *testing.T) {
	root := t.TempDir()
	storeVersion(t, root, "v1.4.0")
	if err := os.MkdirAll(filepath.Join(root, "versions", "broken"), 0o700); err != nil {
		t.Fatal(err)
	}

	got := executeOffline(t, root, "list")

	if got.code != ExitOK || !strings.Contains(got.stdout, "v1.4.0") {
		t.Errorf("壊れた保持版のせいで一覧を表示できない: code = %d\n%s%s", got.code, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stderr, "broken") {
		t.Errorf("壊れた保持版を知らせていない: %s", got.stderr)
	}
}

func TestListJSON(t *testing.T) {
	root := t.TempDir()
	storeVersion(t, root, "v1.4.0")

	got := executeOffline(t, root, "list", "--json")

	var body struct {
		Root   string `json:"root"`
		Stored []struct {
			ID  string `json:"id"`
			Tag string `json:"tag"`
		} `json:"stored"`
		Problems []any `json:"problems"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &body); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, got.stdout)
	}
	if body.Root != root || len(body.Stored) != 1 || body.Stored[0].Tag != "v1.4.0" || body.Problems == nil {
		t.Errorf("JSON の内容が正しくない: %+v", body)
	}
}
