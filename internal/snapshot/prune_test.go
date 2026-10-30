package snapshot

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestRemove(t *testing.T) {
	root, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
	writeTree(t, data, map[string]string{"a": "1"})
	first, _ := Create(root, data, Meta{Reason: ReasonManual}, now)
	second, _ := Create(root, data, Meta{Reason: ReasonManual}, now.Add(time.Minute))

	if err := Remove(root, first.ID); err != nil {
		t.Fatal(err)
	}

	if listed, _, _ := List(root); len(listed) != 1 || listed[0].ID != second.ID {
		t.Errorf("指定したものだけを消していない: %+v", listed)
	}
	if err := Remove(root, first.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("ないものを消そうとしたときに ErrNotFound を返さない: %v", err)
	}
}

func TestPrune(t *testing.T) {
	root, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
	writeTree(t, data, map[string]string{"a": "1"})
	create := func(reason Reason, minutes int) string {
		t.Helper()
		m, err := Create(root, data, Meta{Reason: reason}, now.Add(time.Duration(minutes)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	manual := create(ReasonManual, 0)
	oldAuto := []string{create(ReasonBeforeUse, 1), create(ReasonBeforeRestore, 2), create(ReasonBeforeUse, 3)}
	newAuto := []string{create(ReasonBeforeRestore, 4), create(ReasonBeforeUse, 5)}
	exported := filepath.Join(t.TempDir(), "old.vmeltype-snapshot")
	if err := Export(root, oldAuto[0], exported); err != nil {
		t.Fatal(err)
	}
	imported, err := Import(root, exported, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("0 なら何も消さない", func(t *testing.T) {
		if removed, err := Prune(root, 0); err != nil || len(removed) != 0 {
			t.Errorf("removed = %+v, %v", removed, err)
		}
	})
	t.Run("自動で作ったものの古い分だけを消す", func(t *testing.T) {
		removed, err := Prune(root, 2)
		if err != nil {
			t.Fatal(err)
		}

		var removedIDs []string
		for _, m := range removed {
			removedIDs = append(removedIDs, m.ID)
		}
		slices.Sort(removedIDs)
		if !slices.Equal(removedIDs, oldAuto) {
			t.Errorf("消したもの = %v, want %v", removedIDs, oldAuto)
		}
		listed, _, _ := List(root)
		var left []string
		for _, m := range listed {
			left = append(left, m.ID)
		}
		slices.Sort(left)
		want := []string{manual, newAuto[0], newAuto[1], imported.ID}
		slices.Sort(want)
		if !slices.Equal(left, want) {
			t.Errorf("残ったもの = %v, want %v (手動と取り込んだものは数えない)", left, want)
		}
	})
}

func TestPruneKeepsGivenIDs(t *testing.T) {
	root, data := t.TempDir(), filepath.Join(t.TempDir(), "data")
	writeTree(t, data, map[string]string{"a": "1"})
	var ids []string
	for i := range 3 {
		m, _ := Create(root, data, Meta{Reason: ReasonBeforeUse}, now.Add(time.Duration(i)*time.Minute))
		ids = append(ids, m.ID)
	}

	removed, err := Prune(root, 1, ids[0])

	if err != nil || len(removed) != 1 || removed[0].ID != ids[1] {
		t.Errorf("removed = %+v, %v (いま復元したものは消さない)", removed, err)
	}
}
