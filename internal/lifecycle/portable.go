package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/otnc/vmeltype/internal/archive"
	"github.com/otnc/vmeltype/internal/assets"
	"github.com/otnc/vmeltype/internal/privatefs"
	"github.com/otnc/vmeltype/internal/store"
)

var (
	// ErrEntryConflict は、引き継ぐファイルと新しい版のファイルの名前がぶつかることを表す。
	ErrEntryConflict = errors.New("実行先にある管理していないファイルが、新しい版のファイルと同じ名前です")
	// ErrCorruptStored は、保持版のファイルが manifest と一致しないことを表す。
	ErrCorruptStored = errors.New("保持版のファイルが manifest と一致しません。\"vmeltype install <tag> --reinstall\" で取得し直してください")
	// ErrNeedsRecover は、切り替えの途中で失敗し、recover での復旧が要ることを表す。
	ErrNeedsRecover = errors.New("切り替えの途中で失敗しました。\"vmeltype recover\" で復旧してください")
)

// PortableAppDir は、portable 版の実行先。版を切り替えても場所を変えず、隣の data を引き継ぐ。
func PortableAppDir(root string) string {
	return filepath.Join(root, "portable", "app")
}

// UsePlan は、portable 版の use の計画。
type UsePlan struct {
	Target store.Stored
	AppDir string
	From   *Activation
	// Carried は、前の実行先から引き継ぐ名前 (data と、vMeltype が置いていないもの)。
	Carried []string
	// UpToDate は、同じ版が有効で、実行先のファイルが manifest と一致していること。
	UpToDate bool
}

// PlanPortableUse は、実行先を変えずに、切り替えの計画を作る。固定に反するか、名前がぶつかるなら、この時点で止める。
// overridePin は、利用者がこの操作に限って固定を越えると明示したときだけ true にする。
func PlanPortableUse(root string, target store.Stored, overridePin bool) (UsePlan, error) {
	state, err := LoadState(root)
	if err != nil {
		return UsePlan{}, err
	}
	if !overridePin {
		if err := checkPin(state, target.ID); err != nil {
			return UsePlan{}, err
		}
	}
	appDir := PortableAppDir(root)
	var owned []string
	if state.Active != nil && state.Active.AppDir == appDir {
		owned = state.Active.OwnedEntries
	}

	entries, err := os.ReadDir(appDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return UsePlan{}, err
	}
	var carried []string
	for _, e := range entries {
		if !containsFold(owned, e.Name()) {
			carried = append(carried, e.Name())
		}
	}
	var conflicts []string
	for _, name := range topLevelEntries(appFiles(target)) {
		if containsFold(carried, name) {
			conflicts = append(conflicts, name)
		}
	}
	if len(conflicts) > 0 {
		return UsePlan{}, fmt.Errorf("%w: %s (%s)", ErrEntryConflict, strings.Join(conflicts, "、"), appDir)
	}

	plan := UsePlan{Target: target, AppDir: appDir, From: state.Active, Carried: carried}
	if state.Active != nil && state.Active.StoreID == target.ID {
		plan.UpToDate = deployedMatches(appDir, appFiles(target))
	}
	return plan, nil
}

// ExecutePortableUse は、新しい版を作業用のディレクトリで組み立ててから、rename で実行先を入れ替える。
// 呼び出し側は、管理ルートの排他を取り、未完了の処理記録がないことと、実行先の Meltype が止まっていることを確かめておく。
//
// 前の実行先を退避する前に失敗したら、実行先は変えずに作業用のものを消す。
// 退避したあとに失敗したら、できる限りその場で戻し、戻せなければ処理記録を残して ErrNeedsRecover を返す。
func ExecutePortableUse(root string, storedDir string, plan UsePlan, now time.Time) error {
	parent := filepath.Dir(plan.AppDir)
	if err := privatefs.MkdirAll(parent); err != nil {
		return err
	}
	// rename で入れ替えるため、作業用と退避先は実行先と同じディレクトリに作る
	staging, err := os.MkdirTemp(parent, ".staging-*")
	if err != nil {
		return err
	}
	if err := copyStoredFiles(filepath.Join(storedDir, "files"), staging, plan.Target); err != nil {
		os.RemoveAll(staging)
		return err
	}

	journal := &Journal{
		Operation: "use",
		StartedAt: now.UTC(),
		From:      plan.From,
		To: Activation{
			StoreID:      plan.Target.ID,
			Tag:          plan.Target.Tag,
			Variant:      plan.Target.Variant,
			AppDir:       plan.AppDir,
			OwnedEntries: topLevelEntries(appFiles(plan.Target)),
			ActivatedAt:  now.UTC(),
		},
		StagingDir: staging,
		Carried:    nonNil(plan.Carried),
	}
	_, statErr := os.Stat(plan.AppDir)
	hasOld := statErr == nil
	if hasOld {
		journal.BackupDir = filepath.Join(parent, ".previous-"+strings.TrimPrefix(filepath.Base(staging), ".staging-"))
	}
	if err := journal.advance(root, PhaseStaged); err != nil {
		os.RemoveAll(staging)
		return err
	}

	if hasOld {
		if err := os.Rename(plan.AppDir, journal.BackupDir); err != nil {
			os.RemoveAll(staging)
			clearJournal(root)
			return fmt.Errorf("前の実行先を退避できませんでした。実行先は変えていません: %w", err)
		}
		if err := journal.advance(root, PhaseOldMoved); err != nil {
			return errors.Join(ErrNeedsRecover, err)
		}
	}

	if err := os.Rename(staging, plan.AppDir); err != nil {
		if hasOld {
			if undoErr := os.Rename(journal.BackupDir, plan.AppDir); undoErr != nil {
				return errors.Join(ErrNeedsRecover, err, undoErr)
			}
		}
		os.RemoveAll(staging)
		clearJournal(root)
		return fmt.Errorf("新しい版を実行先に置けませんでした。実行先は元に戻しました: %w", err)
	}
	if err := journal.advance(root, PhaseNewPlaced); err != nil {
		return errors.Join(ErrNeedsRecover, err)
	}

	for _, name := range plan.Carried {
		if err := os.Rename(filepath.Join(journal.BackupDir, name), filepath.Join(plan.AppDir, name)); err != nil {
			return errors.Join(ErrNeedsRecover, fmt.Errorf("%s を新しい実行先へ移せませんでした: %w", name, err))
		}
	}
	if err := journal.advance(root, PhaseCarried); err != nil {
		return errors.Join(ErrNeedsRecover, err)
	}
	return finishUse(root, journal)
}

// finishUse は、有効版の記録を更新し、処理記録と退避した前の版のファイルを消す。
func finishUse(root string, journal *Journal) error {
	state, err := LoadState(root)
	if err != nil {
		return errors.Join(ErrNeedsRecover, err)
	}
	if state.Active != nil && state.Active.StoreID != journal.To.StoreID {
		state.Previous = state.Active
	}
	active := journal.To
	state.Active = &active
	// 固定を越えて切り替えたなら、固定を新しい版に移し、有効でない版への固定を残さない
	if state.Pin != nil && state.Pin.StoreID != active.StoreID {
		pin := pinOf(active, active.ActivatedAt)
		state.Pin = &pin
	}
	if err := saveState(root, state); err != nil {
		return errors.Join(ErrNeedsRecover, err)
	}
	if err := clearJournal(root); err != nil {
		return err
	}
	if journal.BackupDir != "" {
		// 退避先に残っているのは前の版のファイルだけで、保持版から作り直せる
		if err := os.RemoveAll(journal.BackupDir); err != nil {
			return fmt.Errorf("切り替えは終わりましたが、前の版のファイル (%s) を消せませんでした: %w", journal.BackupDir, err)
		}
	}
	return nil
}

// copyStoredFiles は、保持版のうち実行先に置くファイルを、照合しながら dstRoot に写す。
func copyStoredFiles(srcRoot, dstRoot string, s store.Stored) error {
	prefix := appPrefix(s.Variant)
	for _, f := range appFiles(s) {
		dst := filepath.Join(dstRoot, filepath.FromSlash(f.Path))
		if err := privatefs.MkdirAll(filepath.Dir(dst)); err != nil {
			return err
		}
		if err := copyVerified(filepath.Join(srcRoot, filepath.FromSlash(prefix+f.Path)), dst, f); err != nil {
			return err
		}
	}
	return nil
}

// appPrefix は、配布物の中で、実行先に置く本体があるディレクトリ。
// 通常版の ZIP は app の下に本体があり、隣にインストール用のスクリプトなどがある。portable 版は ZIP の直下が本体。
func appPrefix(variant string) string {
	if variant == string(assets.Standard) {
		return "app/"
	}
	return ""
}

// appFiles は、保持版のうち実行先に置くファイルを、実行先からの相対パスにして返す。
func appFiles(s store.Stored) []archive.File {
	prefix := appPrefix(s.Variant)
	var files []archive.File
	for _, f := range s.Files {
		if rel, ok := strings.CutPrefix(f.Path, prefix); ok && rel != "" {
			f.Path = rel
			files = append(files, f)
		}
	}
	return files
}

func copyVerified(src, dst string, want archive.File) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("%w: %s を開けません: %v", ErrCorruptStored, want.Path, err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, hash), in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if n != want.Size || hex.EncodeToString(hash.Sum(nil)) != want.SHA256 {
		return fmt.Errorf("%w: %s", ErrCorruptStored, want.Path)
	}
	return nil
}

// deployedMatches は、実行先のファイルが manifest と一致するかを返す。
func deployedMatches(appDir string, files []archive.File) bool {
	return len(mismatchedFiles(appDir, files)) == 0
}

// mismatchedFiles は、実行先にないか、manifest と中身が違うファイルの、/ 区切りのパスを返す。
func mismatchedFiles(appDir string, files []archive.File) []string {
	var mismatched []string
	for _, f := range files {
		if !fileMatches(filepath.Join(appDir, filepath.FromSlash(f.Path)), f) {
			mismatched = append(mismatched, f.Path)
		}
	}
	return mismatched
}

func fileMatches(path string, want archive.File) bool {
	data, err := os.Open(path)
	if err != nil {
		return false
	}
	defer data.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, data)
	return err == nil && n == want.Size && hex.EncodeToString(hash.Sum(nil)) == want.SHA256
}

func topLevelEntries(files []archive.File) []string {
	var names []string
	for _, f := range files {
		name, _, _ := strings.Cut(f.Path, "/")
		if !containsFold(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// containsFold は、Windows のファイル名と同じく、大文字と小文字を区別せずに探す。
func containsFold(names []string, name string) bool {
	return slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, name) })
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
