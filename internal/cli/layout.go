package cli

import (
	"fmt"

	"github.com/otnc/meltool/internal/assets"
	"github.com/otnc/meltool/internal/lifecycle"
)

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

// activeLayout は、有効なバージョンの実行先とデータ先を返す。有効なバージョンがなければ portable 版のものを返す。
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

// reapplyCommand は、有効なバージョンを同じバージョンのまま置き直すコマンドを返す。
func reapplyCommand(active *lifecycle.Activation) string {
	if active.Variant == string(assets.Portable) {
		return fmt.Sprintf("meltool use %s --portable --force", active.Tag)
	}
	return fmt.Sprintf("meltool use %s --force", active.Tag)
}
