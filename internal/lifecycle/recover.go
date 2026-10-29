package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// RecoverPlan は、中断した use をどう復旧するかの計画。
type RecoverPlan struct {
	Journal *Journal
	// Complete は、新しい版を実行先に置いたあとなので、残りを終えること。false なら、元の実行先へ戻す。
	Complete bool
}

// PlanRecover は、未完了の処理記録から復旧の計画を作る。処理記録がなければ nil を返す。
// rename の直後、処理記録を書く前に止まることもあるので、段階だけでなく実際のディレクトリの状態で決める。
func PlanRecover(root string) (*RecoverPlan, error) {
	journal, err := LoadJournal(root)
	if err != nil || journal == nil {
		return nil, err
	}
	if journal.Operation != "use" || journal.To.AppDir == "" {
		return nil, fmt.Errorf("処理記録 (%s) の形式を扱えません。手で確かめてください: %s", journal.Operation, journalPath(root))
	}
	if journal.Phase == PhaseCarried {
		return &RecoverPlan{Journal: journal, Complete: true}, nil
	}
	appExists := exists(journal.To.AppDir)
	stagingExists := exists(journal.StagingDir)
	// 作業用のものが実行先へ移っていれば、新しい版はもう置かれている
	complete := appExists && !stagingExists
	if !appExists && journal.BackupDir != "" && !exists(journal.BackupDir) {
		return nil, fmt.Errorf("実行先も退避した前の実行先も見つかりません。手で確かめてください: %s", journalPath(root))
	}
	return &RecoverPlan{Journal: journal, Complete: complete}, nil
}

// ExecuteRecover は、計画に従って元へ戻すか、残りを終える。呼び出し側は管理ルートの排他を取っておく。
func ExecuteRecover(root string, plan *RecoverPlan) error {
	j := plan.Journal
	if plan.Complete {
		for _, name := range j.Carried {
			src := filepath.Join(j.BackupDir, name)
			dst := filepath.Join(j.To.AppDir, name)
			switch {
			case !exists(src):
				// すでに移してある
			case exists(dst):
				return fmt.Errorf("%s が退避先と実行先の両方にあります。どちらを残すか手で確かめてください: %s", name, src)
			default:
				if err := os.Rename(src, dst); err != nil {
					return fmt.Errorf("%s を実行先へ移せませんでした: %w", name, err)
				}
			}
		}
		return finishUse(root, j)
	}

	if !exists(j.To.AppDir) && j.BackupDir != "" {
		if err := os.Rename(j.BackupDir, j.To.AppDir); err != nil {
			return fmt.Errorf("前の実行先を戻せませんでした: %w", err)
		}
	}
	if err := os.RemoveAll(j.StagingDir); err != nil {
		return fmt.Errorf("作業用のディレクトリを消せませんでした: %w", err)
	}
	return clearJournal(root)
}

func exists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}
