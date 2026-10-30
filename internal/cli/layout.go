package cli

import (
	"errors"
	"fmt"

	"github.com/otnc/vmeltype/internal/assets"
	"github.com/otnc/vmeltype/internal/lifecycle"
)

// errIMERegistered は、Meltype IME を登録済みの PC で、通常版を切り替えようとしたことを表す。
// 新しい版の本体と古い版の IME の DLL が組み合わさらないよう、DLL の入れ替えを作るまでは止める。
var errIMERegistered = errors.New("この PC には Meltype IME が登録されています。登録済みの PC での通常版の切り替え (IME の DLL の入れ替え) には、まだ対応していません")

// layout は、variant ごとの実行先とデータ先。
type layout struct {
	variant string
	appDir  string
	dataDir string
}

// layoutFor は、variant の実行先とデータ先を返す。通常版は、本家と同じく LocalAppData の下にする。
func layoutFor(env *Env, root, variant string) (layout, error) {
	if variant != string(assets.Standard) {
		return layout{variant: variant, appDir: lifecycle.PortableAppDir(root), dataDir: portableDataDir(root)}, nil
	}
	if env.Platform.OS != "windows" {
		return layout{}, fmt.Errorf("この OS (%s) の通常版の切り替えには、まだ対応していません", env.Platform.OS)
	}
	localAppData, err := env.LocalAppData()
	if err != nil {
		return layout{}, err
	}
	return layout{variant: variant, appDir: lifecycle.StandardAppDir(localAppData), dataDir: lifecycle.StandardDataDir(localAppData)}, nil
}

// activeLayout は、有効版の実行先とデータ先を返す。有効版がなければ portable 版のものを返す。
func activeLayout(env *Env, root string) (layout, error) {
	state, err := lifecycle.LoadState(root)
	if err != nil {
		return layout{}, err
	}
	if state.Active == nil {
		return layoutFor(env, root, string(assets.Portable))
	}
	l, err := layoutFor(env, root, state.Active.Variant)
	if err != nil {
		return layout{}, err
	}
	l.appDir = state.Active.AppDir
	return l, nil
}

// checkIMENotRegistered は、通常版の切り替えの前に、Meltype IME が登録されていないことを確かめる。
func checkIMENotRegistered(env *Env) error {
	registered, err := env.IMERegistered()
	if err != nil {
		return err
	}
	if registered {
		return errIMERegistered
	}
	return nil
}

// reapplyCommand は、有効版を同じ版のまま置き直すコマンドを返す。
func reapplyCommand(active *lifecycle.Activation) string {
	if active.Variant == string(assets.Portable) {
		return fmt.Sprintf("vmeltype use %s --portable --force", active.Tag)
	}
	return fmt.Sprintf("vmeltype use %s --force", active.Tag)
}
