package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/otnc/vmeltype/internal/appctl"
	"github.com/otnc/vmeltype/internal/archive"
	"github.com/otnc/vmeltype/internal/assets"
	"github.com/otnc/vmeltype/internal/store"
	"github.com/otnc/vmeltype/internal/winime"
)

type localEnv struct {
	root    string
	running []string
	// stopFails は、終了の合図に応えない Meltype を真似る。
	stopFails bool
	// stopped と started は、実行先の Meltype を終了させた回数と、起動した回数。
	stopped, started int
	// localAppData は、通常版の実行先とデータ先を置く、利用者の LocalAppData の代わり。
	localAppData string
	// imeRegistered は、Meltype IME を PC に登録済みであることを真似る。
	imeRegistered bool
	// imeResult は、IME の DLL の入れ替えの結果。imeCalls は、入れ替えを求められた記録。
	imeResult winime.Result
	imeCalls  []imeCall
	// upstream は、公式一覧と配布物を返す偽の GitHub。nil なら通信しない。
	upstream *fakeUpstream
	// removeCalls は、本家のアンインストーラーで Meltype の削除を求められた記録。removeFails なら削除に失敗する。
	removeCalls []removeCall
	removeFails bool
}

func newLocalEnv(t *testing.T) *localEnv {
	return &localEnv{root: t.TempDir(), localAppData: t.TempDir()}
}

func (l *localEnv) run(t *testing.T, interactive bool, stdin string, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := Env{
		In:           strings.NewReader(stdin),
		Out:          &stdout,
		Err:          &stderr,
		Interactive:  interactive,
		Version:      "1.2.3",
		Now:          func() time.Time { return time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC) },
		Getenv:       func(string) string { return "" },
		DefaultRoot:  func() (string, error) { return l.root, nil },
		Platform:     assets.Platform{OS: "windows", Arch: "amd64"},
		RunningUnder: func(string) ([]string, error) { return l.running, nil },
		StopApp: func(string) error {
			if l.stopFails {
				return appctl.ErrStillRunning
			}
			l.stopped++
			l.running = nil
			return nil
		},
		StartApp: func(string) error {
			l.started++
			return nil
		},
		LocalAppData:  func() (string, error) { return l.localAppData, nil },
		IMERegistered: func() (bool, error) { return l.imeRegistered, nil },
		Executable:    func() (string, error) { return `C:	oolsvmeltype.exe`, nil },
		RemoveMeltype: func(appDir, fallback string, interactive bool) error {
			l.removeCalls = append(l.removeCalls, removeCall{appDir: appDir, fallback: fallback, interactive: interactive})
			if l.removeFails {
				return errors.New("本家のアンインストーラーが失敗しました")
			}
			return os.RemoveAll(appDir)
		},
		ReplaceIME: func(appDir string) (winime.Result, error) {
			l.imeCalls = append(l.imeCalls, imeCall{appDir: appDir, deployed: readFile(filepath.Join(l.standardAppDir(), "Meltype.exe"))})
			return l.imeResult, nil
		},
	}
	if l.upstream != nil {
		env.HTTPClient = l.upstream.server.Client()
		env.GitHubAPIURL = l.upstream.server.URL
	}
	code := Run(args, env)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// storeFiles は、取得を経ずに保持版を置く。files は、/ 区切りのパスと中身。
func (l *localEnv) storeFiles(t *testing.T, tag, variant string, files map[string]string) {
	t.Helper()
	s := store.Store{Root: l.root}
	staging, err := s.Stage()
	if err != nil {
		t.Fatal(err)
	}
	var records []archive.File
	for path, body := range files {
		dst := filepath.Join(staging.Dir(), "files", filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		records = append(records, archive.File{Path: path, Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	err = staging.Commit(store.Stored{
		ID: store.ID(tag, "windows", "amd64", variant), Tag: tag, OS: "windows", Arch: "amd64", Variant: variant, Files: records,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (l *localEnv) storePortable(t *testing.T, tag string, extra map[string]string) {
	t.Helper()
	files := map[string]string{"Meltype.exe": "exe " + tag, "Meltype.portable": "portable", "dotnet/host.dll": "dll " + tag}
	for k, v := range extra {
		files[k] = v
	}
	l.storeFiles(t, tag, "portable", files)
}

func (l *localEnv) appDir() string {
	return filepath.Join(l.root, "portable", "app")
}

func (l *localEnv) read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(l.appDir(), filepath.FromSlash(rel)))
	if err != nil {
		return "<なし>"
	}
	return string(data)
}

func TestUsePlacesPortableVersion(t *testing.T) {
	l := newLocalEnv(t)
	l.storePortable(t, "v1.4.0", nil)

	got := l.run(t, false, "", "use", "1.4.0", "--portable", "-y")

	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr = %s", got.code, got.stderr)
	}
	if l.read(t, "Meltype.exe") != "exe v1.4.0" || l.read(t, "dotnet/host.dll") != "dll v1.4.0" {
		t.Errorf("実行先に版のファイルがない")
	}
	current := l.run(t, false, "", "current")
	if current.code != ExitOK || !strings.Contains(current.stdout, "v1.4.0") {
		t.Errorf("current が有効版を示していない: %d\n%s%s", current.code, current.stdout, current.stderr)
	}
}

func TestUseSwitchKeepsDataAndUnknownFiles(t *testing.T) {
	l := newLocalEnv(t)
	l.storePortable(t, "v1.4.0", map[string]string{"only-in-1.4.txt": "old"})
	l.storePortable(t, "v1.3.0", nil)
	l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")
	os.MkdirAll(filepath.Join(l.appDir(), "data"), 0o700)
	os.WriteFile(filepath.Join(l.appDir(), "data", "userdict.txt"), []byte("辞書"), 0o600)
	os.WriteFile(filepath.Join(l.appDir(), "memo.txt"), []byte("利用者のメモ"), 0o600)

	got := l.run(t, false, "", "u", "v1.3.0", "--portable", "-y")

	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr = %s", got.code, got.stderr)
	}
	if l.read(t, "Meltype.exe") != "exe v1.3.0" {
		t.Errorf("新しい版のファイルになっていない: %q", l.read(t, "Meltype.exe"))
	}
	if l.read(t, "data/userdict.txt") != "辞書" || l.read(t, "memo.txt") != "利用者のメモ" {
		t.Errorf("data か、管理していないファイルを引き継いでいない")
	}
	if l.read(t, "only-in-1.4.txt") != "<なし>" {
		t.Errorf("前の版だけのファイルが残っている")
	}
	entries, _ := os.ReadDir(filepath.Join(l.root, "portable"))
	if len(entries) != 1 {
		t.Errorf("作業用や退避のディレクトリが残っている: %v", entries)
	}

	var current struct {
		Active   struct{ Tag string } `json:"active"`
		Previous struct{ Tag string } `json:"previous"`
	}
	json.Unmarshal([]byte(l.run(t, false, "", "current", "--json").stdout), &current)
	if current.Active.Tag != "v1.3.0" || current.Previous.Tag != "v1.4.0" {
		t.Errorf("有効版と直前の版の記録が違う: %+v", current)
	}
}

func TestUseSameVersion(t *testing.T) {
	l := newLocalEnv(t)
	l.storePortable(t, "v1.4.0", nil)
	l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")

	t.Run("配置が一致していれば、確認せずに何もしない", func(t *testing.T) {
		got := l.run(t, false, "", "use", "v1.4.0", "--portable")
		if got.code != ExitOK || !strings.Contains(got.stdout, "既に") {
			t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
		}
	})
	t.Run("配置が壊れていれば入れ替える", func(t *testing.T) {
		os.WriteFile(filepath.Join(l.appDir(), "Meltype.exe"), []byte("壊れた"), 0o600)
		got := l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")
		if got.code != ExitOK || l.read(t, "Meltype.exe") != "exe v1.4.0" {
			t.Errorf("code = %d, Meltype.exe = %q, stderr = %s", got.code, l.read(t, "Meltype.exe"), got.stderr)
		}
	})
	t.Run("--force は一致していても入れ替える", func(t *testing.T) {
		for _, flag := range []string{"--force", "-f", "--reinstall"} {
			got := l.run(t, false, "", "use", "v1.4.0", "--portable", flag, "-y")
			if got.code != ExitOK || strings.Contains(got.stdout, "既に") {
				t.Errorf("%s: code = %d\n%s%s", flag, got.code, got.stdout, got.stderr)
			}
		}
	})
}

func TestUseStopsBeforeChanging(t *testing.T) {
	tests := []struct {
		name     string
		prepare  func(t *testing.T, l *localEnv)
		args     []string
		wantCode int
		wantText string
	}{
		{
			name:     "保持していない版は install を案内する",
			args:     []string{"use", "v9.9.9", "--portable", "-y"},
			wantCode: ExitError,
			wantText: "vmeltype install",
		},
		{
			name: "管理していないファイルが新しい版のファイルと同じ名前なら止める",
			prepare: func(t *testing.T, l *localEnv) {
				os.WriteFile(filepath.Join(l.appDir(), "new-file.txt"), []byte("利用者のもの"), 0o600)
				l.storePortable(t, "v1.5.0", map[string]string{"new-file.txt": "版のもの"})
			},
			args:     []string{"use", "v1.5.0", "--portable", "-y"},
			wantCode: ExitError,
			wantText: "new-file.txt",
		},
		{
			name: "保持版のファイルが manifest と一致しなければ止める",
			prepare: func(t *testing.T, l *localEnv) {
				path := filepath.Join(store.Store{Root: l.root}.Path(store.ID("v1.3.0", "windows", "amd64", "portable")), "files", "Meltype.exe")
				os.WriteFile(path, []byte("書き換えられた"), 0o600)
			},
			args:     []string{"use", "v1.3.0", "--portable", "-y"},
			wantCode: ExitError,
			wantText: "--reinstall",
		},
		{
			name: "未完了の処理記録があれば止める",
			prepare: func(t *testing.T, l *localEnv) {
				os.MkdirAll(filepath.Join(l.root, "transactions"), 0o700)
				os.WriteFile(filepath.Join(l.root, "transactions", "current.json"), []byte(`{"schemaVersion":1,"phase":"staged"}`), 0o600)
			},
			args:     []string{"use", "v1.3.0", "--portable", "-y"},
			wantCode: ExitNeedsAction,
			wantText: "recover",
		},
		{
			name:     "非 TTY で -y がなければ確認できずに止める",
			args:     []string{"use", "v1.3.0", "--portable"},
			wantCode: ExitNeedsAction,
		},
		{
			name:     "--dry-run は計画だけを表示する",
			args:     []string{"use", "v1.3.0", "--portable", "--dry-run"},
			wantCode: ExitOK,
			wantText: "v1.3.0",
		},
		{
			name: "保持版に実行先へ置く本体がなければ止める",
			prepare: func(t *testing.T, l *localEnv) {
				l.storeFiles(t, "v1.2.0", "standard", map[string]string{"Meltype.exe": "x"})
			},
			args:     []string{"use", "v1.2.0", "-y"},
			wantCode: ExitError,
			wantText: "本体",
		},
		{
			name:     "portable 版だけを保持していても、--portable がなければ選ばない",
			args:     []string{"use", "v1.3.0", "-y"},
			wantCode: ExitError,
			wantText: "--portable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLocalEnv(t)
			l.storePortable(t, "v1.4.0", nil)
			l.storePortable(t, "v1.3.0", nil)
			l.run(t, false, "", "use", "v1.4.0", "--portable", "-y")
			if tt.prepare != nil {
				tt.prepare(t, l)
			}

			got := l.run(t, false, "", tt.args...)

			if got.code != tt.wantCode {
				t.Errorf("code = %d, want %d\n%s%s", got.code, tt.wantCode, got.stdout, got.stderr)
			}
			if !strings.Contains(got.stdout+got.stderr, tt.wantText) {
				t.Errorf("%q を示していない:\n%s%s", tt.wantText, got.stdout, got.stderr)
			}
			if l.read(t, "Meltype.exe") != "exe v1.4.0" {
				t.Errorf("止まるべきなのに実行先を変えた: %q", l.read(t, "Meltype.exe"))
			}
		})
	}
}

func TestVariantSelectionWhenBothAreStored(t *testing.T) {
	setup := func(t *testing.T) *localEnv {
		l := newLocalEnv(t)
		l.storePortable(t, "v1.4.0", nil)
		l.storeFiles(t, "v1.4.0", "standard", map[string]string{"Meltype.exe": "x"})
		return l
	}

	t.Run("--portable がなければ通常版を対象にする", func(t *testing.T) {
		got := setup(t).run(t, false, "", "use", "v1.4.0", "-y")
		if !strings.Contains(got.stderr, "v1.4.0_windows-amd64_standard") {
			t.Errorf("通常版を選んでいない: code = %d, stderr = %s", got.code, got.stderr)
		}
	})
	t.Run("--portable で portable 版を対象にする", func(t *testing.T) {
		got := setup(t).run(t, false, "", "use", "v1.4.0", "--portable", "-y")
		if got.code != ExitOK {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
	t.Run("--id の variant と --portable が合わなければ止める", func(t *testing.T) {
		got := setup(t).run(t, false, "", "use", "--id", "v1.4.0_windows-amd64_standard", "--portable", "-y")
		if got.code != ExitUsage {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
	t.Run("uninstall も --portable がなければ通常版を削除する", func(t *testing.T) {
		l := setup(t)
		got := l.run(t, false, "", "uninstall", "v1.4.0", "-y")
		stored := storedIDs(t, l.root)
		if got.code != ExitOK || len(stored) != 1 || stored[0] != "v1.4.0_windows-amd64_portable" {
			t.Errorf("code = %d, 残った保持版 = %v, stderr = %s", got.code, stored, got.stderr)
		}
	})
}

func storedIDs(t *testing.T, root string) []string {
	t.Helper()
	stored, _, err := store.Store{Root: root}.List()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range stored {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestCurrentWithoutActiveVersion(t *testing.T) {
	got := newLocalEnv(t).run(t, false, "", "current")

	if got.code != ExitOK || !strings.Contains(got.stdout, "有効な版はありません") {
		t.Errorf("code = %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

func TestUseVersionQuery(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "版を省くと、保持している正式版の最新", args: []string{"use", "--portable"}, want: "exe v1.4.0"},
		{name: "範囲の指定で、保持している中の正式版の最新", args: []string{"use", "1.3", "--portable"}, want: "exe v1.3.0"},
		{name: "--pre で、保持しているプレリリースの最新", args: []string{"use", "--pre", "--portable"}, want: "exe v1.4.1-pre.1"},
		{name: "v を省いた版", args: []string{"use", "1.3.0", "--portable"}, want: "exe v1.3.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newLocalEnv(t)
			for _, tag := range []string{"v1.3.0", "v1.4.0", "v1.4.1-pre.1"} {
				l.storePortable(t, tag, nil)
			}

			got := l.run(t, false, "", append(tt.args, "-y")...)

			if got.code != ExitOK || l.read(t, "Meltype.exe") != tt.want {
				t.Errorf("code = %d, Meltype.exe = %q, want %q\n%s", got.code, l.read(t, "Meltype.exe"), tt.want, got.stderr)
			}
		})
	}

	t.Run("合う保持版がなければ、指定の意味と install を示す", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storePortable(t, "v1.4.0", nil)
		got := l.run(t, false, "", "use", "1.3", "--portable", "-y")
		if got.code != ExitError || !strings.Contains(got.stderr, "1.3.x の正式版の最新") || !strings.Contains(got.stderr, "vmeltype install 1.3 --portable") {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
}

func TestUninstallRequiresExactVersion(t *testing.T) {
	for _, args := range [][]string{{"1.3"}, {"1"}, {"latest"}, {"1.3.0", "--pre"}} {
		t.Run(strings.Join(args, " ")+" は引数の誤り", func(t *testing.T) {
			l := newLocalEnv(t)
			l.storePortable(t, "v1.3.0", nil)

			got := l.run(t, false, "", append(append([]string{"uninstall"}, args...), "--portable", "-y")...)

			if got.code != ExitUsage {
				t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
			}
			if len(storedTags(t, l.root)) != 1 {
				t.Errorf("範囲の指定で保持版を削除した")
			}
		})
	}

	t.Run("v を省いた版で削除できる", func(t *testing.T) {
		l := newLocalEnv(t)
		l.storePortable(t, "v1.3.0", nil)
		got := l.run(t, false, "", "uninstall", "1.3.0", "--portable", "-y")
		if got.code != ExitOK || len(storedTags(t, l.root)) != 0 {
			t.Errorf("code = %d, stderr = %s", got.code, got.stderr)
		}
	})
}

// imeCall は、IME の DLL の入れ替えを求められたときの、渡された切り替え先の版と、そのときの実行先の Meltype.exe の中身。
type imeCall struct {
	appDir   string
	deployed string
}

// removeCall は、本家のアンインストーラーで Meltype の削除を求められたときに渡されたもの。
type removeCall struct {
	appDir, fallback string
	interactive      bool
}
