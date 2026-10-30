package lifecycle

import (
	"github.com/otnc/vmeltype/internal/store"
)

// Deployment は、実行先の配置が、保持バージョンのうち有効なものと一致するか。
type Deployment string

const (
	// DeploymentNone は、有効なバージョンがないこと。
	DeploymentNone Deployment = "none"
	// DeploymentMatch は、保持バージョンのファイルがすべて実行先にあり、中身も一致すること。
	DeploymentMatch Deployment = "match"
	// DeploymentMismatch は、実行先にないか、中身の違うファイルがあること。
	DeploymentMismatch Deployment = "mismatch"
	// DeploymentUnknown は、保持バージョンのうち有効なものを読めず、比べられないこと。
	DeploymentUnknown Deployment = "unknown"
	// DeploymentPending は、中断した変更があり、実行先が定まっていないので比べないこと。
	DeploymentPending Deployment = "pending"
)

// Status は、管理ルートのいまの状態。記録を読んで実行先を調べるだけで、何も変えない。
type Status struct {
	Active     *Activation `json:"active"`
	Previous   *Activation `json:"previous"`
	Pin        *Pinned     `json:"pin"`
	Deployment Deployment  `json:"deployment"`
	// Mismatched は、実行先にないか、保持バージョンと中身が違うファイルの、/ 区切りのパス。
	Mismatched []string `json:"mismatched"`
	// Pending は、中断した変更の処理記録。なければ nil。
	Pending *Journal `json:"pending"`
}

// Inspect は、有効なバージョンの記録と処理記録を読み、実行先の配置を保持バージョンと比べる。排他は取らない。
func Inspect(root string) (Status, error) {
	state, err := LoadState(root)
	if err != nil {
		return Status{}, err
	}
	journal, err := LoadJournal(root)
	if err != nil {
		return Status{}, err
	}
	s := Status{Active: state.Active, Previous: state.Previous, Pin: state.Pin, Pending: journal, Mismatched: []string{}}
	switch {
	case journal != nil:
		s.Deployment = DeploymentPending
	case state.Active == nil:
		s.Deployment = DeploymentNone
	default:
		stored, err := store.Store{Root: root}.Get(state.Active.StoreID)
		if err != nil {
			s.Deployment = DeploymentUnknown
			break
		}
		if mismatched := mismatchedFiles(state.Active.AppDir, appFiles(stored)); len(mismatched) > 0 {
			s.Deployment = DeploymentMismatch
			s.Mismatched = mismatched
		} else {
			s.Deployment = DeploymentMatch
		}
	}
	return s, nil
}
