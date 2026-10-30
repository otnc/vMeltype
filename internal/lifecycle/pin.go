package lifecycle

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrPinned は、固定している版から別の版へ切り替えようとしたことを表す。
	ErrPinned = errors.New("版を固定しているので、別の版に切り替えません")
	// ErrNoActive は、有効版がなく、固定できないことを表す。
	ErrNoActive = errors.New("有効な版がないので固定できません。先に \"vmeltype use <tag>\" で有効にしてください")
)

// Pinned は、固定した版。同じタグでも variant や配布物が違えば別の版として扱うため、保持 ID で比べる。
type Pinned struct {
	StoreID  string    `json:"storeId"`
	Tag      string    `json:"tag"`
	Variant  string    `json:"variant"`
	PinnedAt time.Time `json:"pinnedAt"`
}

// Pin は、有効版を固定する。呼び出し側は、管理ルートの排他を取り、未完了の処理記録がないことを確かめておく。
func Pin(root string, now time.Time) (Pinned, error) {
	state, err := LoadState(root)
	if err != nil {
		return Pinned{}, err
	}
	if state.Active == nil {
		return Pinned{}, ErrNoActive
	}
	pin := pinOf(*state.Active, now)
	state.Pin = &pin
	if err := saveState(root, state); err != nil {
		return Pinned{}, err
	}
	return pin, nil
}

// Unpin は、固定だけを解除する。固定していなければ false を返す。呼び出し側は、管理ルートの排他を取っておく。
func Unpin(root string) (bool, error) {
	state, err := LoadState(root)
	if err != nil || state.Pin == nil {
		return false, err
	}
	state.Pin = nil
	return true, saveState(root, state)
}

// checkPin は、固定していれば、別の版への切り替えを ErrPinned で止める。同じ版の再適用は止めない。
func checkPin(state State, targetID string) error {
	if state.Pin == nil || state.Pin.StoreID == targetID {
		return nil
	}
	return fmt.Errorf("%w。%s (%s) に固定しています。切り替えるには \"vmeltype unpin\" で解除するか、この操作だけ --override-pin を付けてください", ErrPinned, state.Pin.Tag, state.Pin.StoreID)
}

func pinOf(a Activation, now time.Time) Pinned {
	return Pinned{StoreID: a.StoreID, Tag: a.Tag, Variant: a.Variant, PinnedAt: now.UTC()}
}
