package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrPendingJournal は、中断した変更の処理記録が残っていて、新しい変更を始められないことを表す。
var ErrPendingJournal = errors.New("中断した変更の処理記録が残っています。\"vmeltype recover\" で復旧してから実行してください")

// 処理記録の段階。recover は、どこまで進んだかで、戻すか終えるかを決める。
const (
	// PhaseStaged は、新しい版を作業用のディレクトリに組み立てた段階。実行先はまだ変えていない。
	PhaseStaged = "staged"
	// PhaseOldMoved は、前の実行先を退避した段階。実行先は空になっている。
	PhaseOldMoved = "old-moved"
	// PhaseNewPlaced は、新しい版を実行先に置いた段階。data などは、まだ退避先にある。
	PhaseNewPlaced = "new-placed"
	// PhaseCarried は、data などを新しい実行先へ戻し終えた段階。
	PhaseCarried = "carried"
)

// Journal は、use の処理記録。
type Journal struct {
	SchemaVersion int         `json:"schemaVersion"`
	Operation     string      `json:"operation"`
	StartedAt     time.Time   `json:"startedAt"`
	Phase         string      `json:"phase"`
	From          *Activation `json:"from,omitempty"`
	To            Activation  `json:"to"`
	StagingDir    string      `json:"stagingDir"`
	BackupDir     string      `json:"backupDir,omitempty"`
	// Carried は、前の実行先から新しい実行先へ引き継ぐ名前 (data など)。
	Carried []string `json:"carried"`
	// Adopted は、取り込んだ本家のインストールのうち置き換えた名前。空でなければ、退避したものを管理前の記録として残す。
	Adopted []string `json:"adopted,omitempty"`
}

func journalPath(root string) string {
	return filepath.Join(root, "transactions", "current.json")
}

// LoadJournal は、未完了の処理記録を返す。なければ nil を返す。
func LoadJournal(root string) (*Journal, error) {
	var j Journal
	if err := readJSON(journalPath(root), &j); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("処理記録を読めませんでした: %w", err)
	}
	if j.SchemaVersion > SchemaVersion {
		return nil, errNewerSchema
	}
	return &j, nil
}

func (j *Journal) advance(root, phase string) error {
	j.Phase = phase
	j.SchemaVersion = SchemaVersion
	if err := writeJSON(journalPath(root), j); err != nil {
		return fmt.Errorf("処理記録を書けませんでした: %w", err)
	}
	return nil
}

func clearJournal(root string) error {
	err := os.Remove(journalPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
