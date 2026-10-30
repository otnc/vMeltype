package lifecycle

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/otnc/meltool/internal/privatefs"
	"github.com/otnc/meltool/internal/snapshot"
	"github.com/otnc/meltool/internal/upstreamconf"
)

// StandardAppDir は、通常版の実行先。本家と同じ場所にし、自動起動のタスク、ショートカット、「設定 → アプリ」の登録をそのまま使う。
func StandardAppDir(localAppData string) string {
	return filepath.Join(localAppData, "Programs", "Meltype")
}

// StandardDataDir は、通常版のデータ先。本家が LocalAppData の下に決めていて、Meltool は変えない。
func StandardDataDir(localAppData string) string {
	return filepath.Join(localAppData, "Meltype")
}

// Baseline は、Meltool が本家を初めて管理する直前の状態。管理前に戻すときに使う。項目ごとに、最初に変えたときだけ記録する。
type Baseline struct {
	SchemaVersion int `json:"schemaVersion"`
	// StandardApp は、取り込んだ本家のインストール。取り込まずに始めたなら nil。
	StandardApp *AdoptedApp `json:"standardApp,omitempty"`
	// StandardConfig は、本家の設定の、Meltool が変える前の値。
	StandardConfig *upstreamconf.Original `json:"standardConfig,omitempty"`
	// QuarantinedUpdates は、本家がダウンロード済みだった更新を移した先。
	QuarantinedUpdates []string `json:"quarantinedUpdates,omitempty"`
}

// AdoptedApp は、取り込んだ本家のインストールのうち、Meltool が置き換えたファイル。
type AdoptedApp struct {
	AppDir string `json:"appDir"`
	// SavedDir は、置き換えた本家のファイルを移した先。
	SavedDir string `json:"savedDir"`
	// Replaced は、置き換えた実行先の直下の名前。ほかのもの (アンインストール用のファイルなど) は実行先に引き継いだ。
	Replaced  []string  `json:"replaced"`
	AdoptedAt time.Time `json:"adoptedAt"`
}

func baselinePath(root string) string {
	return filepath.Join(stateDir(root), "baseline.json")
}

// LoadBaseline は、管理前の記録を読む。まだなければ空の記録を返す。
func LoadBaseline(root string) (Baseline, error) {
	var b Baseline
	if err := readJSON(baselinePath(root), &b); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Baseline{}, nil
		}
		return Baseline{}, fmt.Errorf("管理前の記録を読めませんでした: %w", err)
	}
	if b.SchemaVersion > SchemaVersion {
		return Baseline{}, errNewerSchema
	}
	return b, nil
}

func updateBaseline(root string, change func(*Baseline)) error {
	b, err := LoadBaseline(root)
	if err != nil {
		return err
	}
	change(&b)
	b.SchemaVersion = SchemaVersion
	return writeJSON(baselinePath(root), &b)
}

// PrepareStandardData は、通常版を切り替える前に、データ先を整える。呼び出し側は、Meltype を止めておく。
//
//  1. 本家がダウンロード済みの更新を管理ルートに移す。残すと、本家が次の起動で実行先を上書きする
//  2. データ先のスナップショットを作る
//  3. 本家の自動更新を止める
//
// どれも途中で止まっても、実行先と有効なバージョンは変えていないので、もう一度 use すればよい。
func PrepareStandardData(root, dataDir, activeTag string, now time.Time) error {
	if err := quarantineUpdates(root, dataDir, now); err != nil {
		return err
	}
	_, err := snapshot.Create(root, dataDir, snapshot.Meta{Reason: snapshot.ReasonBeforeUse, ActiveTag: activeTag}, now)
	if err != nil && !errors.Is(err, snapshot.ErrNoData) {
		return fmt.Errorf("切り替えの前にデータを保存できなかったので、切り替えません: %w", err)
	}
	original, err := upstreamconf.DisableAutoUpdate(filepath.Join(dataDir, "config.json"))
	if err != nil {
		return err
	}
	return updateBaseline(root, func(b *Baseline) {
		if b.StandardConfig == nil {
			b.StandardConfig = &original
		}
	})
}

func quarantineUpdates(root, dataDir string, now time.Time) error {
	updates := filepath.Join(dataDir, "update")
	if _, err := os.Stat(updates); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	dir := filepath.Join(root, "quarantine")
	if err := privatefs.MkdirAll(dir); err != nil {
		return err
	}
	dst := filepath.Join(dir, "update-"+now.UTC().Format("20060102T150405Z"))
	for i := 2; exists(dst); i++ {
		dst = filepath.Join(dir, fmt.Sprintf("update-%s-%d", now.UTC().Format("20060102T150405Z"), i))
	}
	// 管理ルートが別のドライブにあれば rename できないので、写してから消す
	if err := os.Rename(updates, dst); err != nil {
		if copyErr := copyTree(updates, dst); copyErr != nil {
			os.RemoveAll(dst)
			return fmt.Errorf("本家がダウンロード済みの更新 (%s) を管理ルートに移せなかったので、切り替えません: %w", updates, errors.Join(err, copyErr))
		}
		if err := os.RemoveAll(updates); err != nil {
			return fmt.Errorf("本家がダウンロード済みの更新 (%s) を管理ルートに写しましたが、消せなかったので、切り替えません: %w", updates, err)
		}
	}
	return updateBaseline(root, func(b *Baseline) { b.QuarantinedUpdates = append(b.QuarantinedUpdates, dst) })
}

// keepAdoptedApp は、取り込んだ本家のファイル (前の実行先を退避したもの) を、管理前の記録として管理ルートに移す。
// 管理ルートが別のドライブにあって rename できなければ、写してから消す。写せなければ、退避した場所のまま記録する。
func keepAdoptedApp(root string, journal *Journal) error {
	b, err := LoadBaseline(root)
	if err != nil {
		return err
	}
	if b.StandardApp != nil {
		return os.RemoveAll(journal.BackupDir)
	}
	saved := filepath.Join(root, "baseline", "standard-app")
	if err := privatefs.MkdirAll(filepath.Dir(saved)); err != nil {
		return err
	}
	if err := os.Rename(journal.BackupDir, saved); err != nil {
		if copyErr := copyTree(journal.BackupDir, saved); copyErr != nil {
			os.RemoveAll(saved)
			saved = journal.BackupDir
		} else {
			os.RemoveAll(journal.BackupDir)
		}
	}
	return updateBaseline(root, func(b *Baseline) {
		b.StandardApp = &AdoptedApp{AppDir: journal.To.AppDir, SavedDir: saved, Replaced: nonNil(journal.Adopted), AdoptedAt: journal.StartedAt}
	})
}

// copyTree は、src の通常のファイルとディレクトリを dst に写す。リンクなど通常のファイルでないものがあれば止める。
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return privatefs.MkdirAll(target)
		case d.Type().IsRegular():
			return copyFile(path, target)
		default:
			return fmt.Errorf("通常のファイルでないものがあります: %s", path)
		}
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
