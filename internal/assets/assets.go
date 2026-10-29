// Package assets は、リリースの配布物から、この端末の OS、CPU、variant に合うものを 1 つに決める。
package assets

import (
	"errors"
	"fmt"
	"strings"

	"github.com/otnc/vmeltype/internal/catalog"
)

// Platform は、Go の GOOS と GOARCH の組。
type Platform struct {
	OS   string
	Arch string
}

// Variant は、通常版と portable 版の区別。
type Variant string

const (
	Standard Variant = "standard"
	Portable Variant = "portable"
)

var (
	// ErrUnsupportedPlatform は、vMeltype がこの OS、CPU、variant の組を扱わないことを表す。
	ErrUnsupportedPlatform = errors.New("この OS、CPU、variant の組には対応していません")
	// ErrNoAsset は、リリースに合う配布物がないことを表す。
	ErrNoAsset = errors.New("この端末に合う配布物がありません")
	// ErrAmbiguousAsset は、合う配布物が複数あり、1 つに決められないことを表す。
	ErrAmbiguousAsset = errors.New("この端末に合う配布物が複数あり、1 つに決められません")
)

// 公式インストーラー (setup.exe) と deb、rpm は、本家の導入処理やパッケージ管理に任せることになるので選ばない。
// 名前の規則は v1.0.0 からの Meltype-<版>-<platform>.zip で、それより前の試験版の形式は扱わない。
var platformSuffixes = map[Platform]map[Variant]string{
	{"windows", "amd64"}: {Standard: "windows", Portable: "windows-portable"},
	{"darwin", "arm64"}:  {Standard: "mac"},
	{"linux", "amd64"}:   {Standard: "linux"},
	{"linux", "arm64"}:   {Standard: "linux-arm64"},
}

var unsupportedReasons = map[Platform]string{
	{"windows", "arm64"}: "本家の IME の登録が ARM64 の Windows を拒否するため",
	{"darwin", "amd64"}:  "本家の macOS の配布物は Apple Silicon 向けだけのため",
}

// Select は、リリースから platform と variant に合う配布物を 1 つ返す。
//
//	asset, err := assets.Select(release, assets.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}, assets.Standard)
func Select(release catalog.Release, platform Platform, variant Variant) (catalog.Asset, error) {
	suffix, ok := platformSuffixes[platform][variant]
	if !ok {
		if reason, known := unsupportedReasons[platform]; known {
			return catalog.Asset{}, fmt.Errorf("%w (%s/%s、%s): %s", ErrUnsupportedPlatform, platform.OS, platform.Arch, variant, reason)
		}
		return catalog.Asset{}, fmt.Errorf("%w (%s/%s、%s)", ErrUnsupportedPlatform, platform.OS, platform.Arch, variant)
	}

	want := fmt.Sprintf("Meltype-%s-%s.zip", strings.TrimPrefix(release.Tag, "v"), suffix)
	var matched []catalog.Asset
	for _, a := range release.Assets {
		if a.Name == want {
			matched = append(matched, a)
		}
	}
	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return catalog.Asset{}, fmt.Errorf("%w (%s に %s がありません)", ErrNoAsset, release.Tag, want)
	default:
		return catalog.Asset{}, fmt.Errorf("%w (%s に %s が %d 件あります)", ErrAmbiguousAsset, release.Tag, want, len(matched))
	}
}
