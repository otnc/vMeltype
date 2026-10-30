package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Remove は、スナップショットを消す。先に . で始まる名前へ移してから消すので、途中で止まっても壊れたスナップショットとして一覧に出ない。
func Remove(root, id string) error {
	if _, err := Get(root, id); err != nil {
		return err
	}
	removing := filepath.Join(snapshotsDir(root), ".removing-"+id)
	if err := os.Rename(Path(root, id), removing); err != nil {
		return fmt.Errorf("スナップショット %s を削除できませんでした: %w", id, err)
	}
	if err := os.RemoveAll(removing); err != nil {
		return fmt.Errorf("スナップショット %s は一覧から外しましたが、ファイル (%s) を消しきれませんでした: %w", id, removing, err)
	}
	return nil
}

// Prune は、切り替えや復元の前に自動で作ったスナップショットを、新しいほうから keep 個だけ残して消し、消したものを返す。
// 手動で作ったものと取り込んだものは、利用者が残すと決めたものなので数えず、消さない。keep が 0 以下なら何も消さない。
// protected (いま復元したものなど) も数えず、消さない。
func Prune(root string, keep int, protected ...string) ([]Manifest, error) {
	targets, err := PruneTargets(root, keep, protected...)
	if err != nil {
		return nil, err
	}
	var removed []Manifest
	for _, m := range targets {
		if err := Remove(root, m.ID); err != nil {
			return removed, err
		}
		removed = append(removed, m)
	}
	return removed, nil
}

// PruneTargets は、Prune が消すスナップショットを、消さずに返す。
func PruneTargets(root string, keep int, protected ...string) ([]Manifest, error) {
	if keep <= 0 {
		return nil, nil
	}
	listed, _, err := List(root)
	if err != nil {
		return nil, err
	}
	var auto []Manifest
	for _, m := range listed {
		if m.Reason != ReasonManual && m.ImportedAt == nil && !slices.Contains(protected, m.ID) {
			auto = append(auto, m)
		}
	}
	if len(auto) <= keep {
		return nil, nil
	}
	return auto[keep:], nil
}
