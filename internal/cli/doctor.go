package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/otnc/vmeltype/internal/assets"
	"github.com/otnc/vmeltype/internal/config"
	"github.com/otnc/vmeltype/internal/lifecycle"
	"github.com/otnc/vmeltype/internal/snapshot"
	"github.com/otnc/vmeltype/internal/store"
	"github.com/otnc/vmeltype/internal/winsys"
)

// errDoctorFailed は、doctor が直すべき問題を見つけたことを表す。
var errDoctorFailed = errors.New("直すべき問題があります。上の対処を行ってください")

type checkLevel string

const (
	levelOK   checkLevel = "ok"
	levelWarn checkLevel = "warn"
	levelFail checkLevel = "fail"
)

var levelLabels = map[checkLevel]string{levelOK: "OK", levelWarn: "警告", levelFail: "要対応"}

// check は、doctor の 1 つの診断。Name は JSON で項目を区別する英語の名前。
type check struct {
	Name    string     `json:"name"`
	Level   checkLevel `json:"level"`
	Message string     `json:"message"`
	Hint    string     `json:"hint,omitempty"`
}

func runDoctor(env *Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		root, err := managementRoot(cmd, env)
		if err != nil {
			return err
		}
		checks := diagnose(env, root)
		ok := true
		for _, c := range checks {
			if c.Level == levelFail {
				ok = false
			}
		}
		out := cmd.OutOrStdout()
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			if err := writeJSON(out, map[string]any{"root": root, "ok": ok, "checks": checks}); err != nil {
				return err
			}
		} else {
			writeChecks(out, root, checks)
		}
		if !ok {
			return errDoctorFailed
		}
		return nil
	}
}

// diagnose は、管理ルートを調べる。1 つの項目を読めなくても、ほかの項目は続けて調べる。
func diagnose(env *Env, root string) []check {
	var checks []check
	s, err := inspectLocal(env, root)
	if err != nil {
		checks = append(checks, check{Name: "state", Level: levelFail, Message: fmt.Sprintf("有効版の記録か処理記録を読めません: %v", err), Hint: "記録のファイルを手で確かめてください"})
	} else {
		checks = append(checks, journalCheck(s), deploymentCheck(s))
		if c, ok := previousCheck(root, s); ok {
			checks = append(checks, c)
		}
		if c, ok := imeCheck(env, root, s); ok {
			checks = append(checks, c)
		}
	}
	checks = append(checks, storedCheck(root), snapshotsCheck(root), configCheck(root))
	return checks
}

func journalCheck(s localStatus) check {
	if s.Pending == nil {
		return check{Name: "journal", Level: levelOK, Message: "中断した変更はありません"}
	}
	return check{Name: "journal", Level: levelFail, Message: describeJournal(s.Pending), Hint: "\"vmeltype recover\" で復旧してください"}
}

func deploymentCheck(s localStatus) check {
	c := check{Name: "deployment"}
	switch s.Deployment {
	case lifecycle.DeploymentNone:
		c.Level, c.Message = levelOK, "有効な版はありません"
	case lifecycle.DeploymentMatch:
		c.Level, c.Message = levelOK, fmt.Sprintf("実行先の %s は保持版と一致しています", s.Active.Tag)
	case lifecycle.DeploymentPending:
		c.Level, c.Message = levelWarn, describeDeployment(s.Status)
	case lifecycle.DeploymentMismatch:
		c.Level, c.Message = levelFail, fmt.Sprintf("実行先の %s が保持版と一致しません (%s)", s.Active.Tag, summarize(s.Mismatched))
		c.Hint = fmt.Sprintf("\"%s\" で置き直してください", reapplyCommand(s.Active))
	default:
		c.Level, c.Message = levelFail, describeDeployment(s.Status)
		c.Hint = fmt.Sprintf("\"%s\" で取得し直してください", installCommand(s.Active.Tag, s.Active.Variant))
	}
	return c
}

// previousCheck は、rollback で戻る先を保持しているかを調べる。直前の版がなければ項目を出さない。
func previousCheck(root string, s localStatus) (check, bool) {
	if s.Previous == nil {
		return check{}, false
	}
	if _, err := (store.Store{Root: root}).Get(s.Previous.StoreID); err != nil {
		return check{Name: "previous", Level: levelWarn, Message: fmt.Sprintf("直前の版 %s を保持していないので、rollback で戻れません", s.Previous.Tag),
			Hint: fmt.Sprintf("戻せるようにするには \"%s\" で取得してください", installCommand(s.Previous.Tag, s.Previous.Variant))}, true
	}
	return check{Name: "previous", Level: levelOK, Message: fmt.Sprintf("直前の版 %s を保持しているので、rollback で戻れます", s.Previous.Tag)}, true
}

func storedCheck(root string) check {
	stored, problems, err := store.Store{Root: root}.List()
	if err != nil {
		return check{Name: "stored", Level: levelFail, Message: fmt.Sprintf("保持版を読めません: %v", err)}
	}
	if len(problems) > 0 {
		var ids []string
		for _, p := range problems {
			ids = append(ids, p.ID)
		}
		return check{Name: "stored", Level: levelWarn, Message: fmt.Sprintf("読めない保持版があります (%s)", strings.Join(ids, "、")),
			Hint: "使うなら \"vmeltype install <tag> --reinstall\" で取得し直してください"}
	}
	return check{Name: "stored", Level: levelOK, Message: fmt.Sprintf("保持版は %d 件で、すべて読めます", len(stored))}
}

func snapshotsCheck(root string) check {
	listed, problems, err := snapshot.List(root)
	if err != nil {
		return check{Name: "snapshots", Level: levelFail, Message: fmt.Sprintf("スナップショットを読めません: %v", err)}
	}
	if len(problems) > 0 {
		var ids []string
		for _, p := range problems {
			ids = append(ids, p.ID)
		}
		return check{Name: "snapshots", Level: levelWarn, Message: fmt.Sprintf("読めないスナップショットがあります (%s)", strings.Join(ids, "、")),
			Hint: "復元には使えません。中身を確かめてから消してください"}
	}
	return check{Name: "snapshots", Level: levelOK, Message: fmt.Sprintf("スナップショットは %d 件で、すべて読めます", len(listed))}
}

func configCheck(root string) check {
	if _, err := config.Load(root); err != nil {
		return check{Name: "config", Level: levelFail, Message: err.Error(), Hint: "\"vmeltype app config set <key> <value>\" で直してください"}
	}
	return check{Name: "config", Level: levelOK, Message: "設定を読めます"}
}

func installCommand(tag, variant string) string {
	if variant == string(assets.Portable) {
		return fmt.Sprintf("vmeltype install %s --portable", tag)
	}
	return "vmeltype install " + tag
}

func writeChecks(w io.Writer, root string, checks []check) {
	fmt.Fprintf(w, "管理ルート: %s\n\n", root)
	for _, c := range checks {
		fmt.Fprintf(w, "[%s] %s\n", levelLabels[c.Level], c.Message)
		if c.Hint != "" {
			fmt.Fprintf(w, "    対処: %s\n", c.Hint)
		}
	}
}

// imeCheck は、有効版が通常版で Meltype IME を登録済みなら、登録されている DLL が有効版のものかを調べる。
// 本家の更新で本体だけが新しくなり、古い版の DLL が残ることがある。新旧の本体と DLL が組み合わさって動くとは限らない。
// 比べるのは、本家が DLL の隣に置く、署名する前の中身のハッシュ。調べないときは項目を出さない。
func imeCheck(env *Env, root string, s localStatus) (check, bool) {
	if s.Active == nil || s.Active.Variant != string(assets.Standard) || s.Pending != nil || env.IMERegistered == nil {
		return check{}, false
	}
	registered, err := env.IMERegistered()
	if err != nil {
		return check{Name: "ime", Level: levelWarn, Message: err.Error()}, true
	}
	if !registered {
		return check{}, false
	}
	installed, err := env.InstalledTipHashes()
	if err != nil {
		return check{Name: "ime", Level: levelWarn, Message: err.Error()}, true
	}
	tip := filepath.Join(store.Store{Root: root}.Path(s.Active.StoreID), "files", "app", "tip")
	var mismatched []string
	for _, arch := range []string{"x64", "x86"} {
		data, err := os.ReadFile(filepath.Join(tip, arch, "MeltypeTip.dll.sha256"))
		expected := winsys.NormalizeHash(string(data))
		if err != nil || expected == "" || installed[arch] == "" {
			return check{Name: "ime", Level: levelWarn, Message: fmt.Sprintf("登録されている Meltype IME の DLL (%s) が %s のものかを、ハッシュがないので確かめられません", arch, s.Active.Tag)}, true
		}
		if installed[arch] != expected {
			mismatched = append(mismatched, arch)
		}
	}
	if len(mismatched) > 0 {
		return check{Name: "ime", Level: levelFail,
			Message: fmt.Sprintf("登録されている Meltype IME の DLL (%s) が、有効版 %s のものではありません", strings.Join(mismatched, "、"), s.Active.Tag),
			Hint:    fmt.Sprintf("\"%s\" で、DLL も %s のものに入れ替えてください", reapplyCommand(s.Active), s.Active.Tag)}, true
	}
	return check{Name: "ime", Level: levelOK, Message: fmt.Sprintf("登録されている Meltype IME の DLL は %s のものです", s.Active.Tag)}, true
}
