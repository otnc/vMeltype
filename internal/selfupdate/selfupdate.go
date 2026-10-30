// Package selfupdate は、Meltool 自身の新しいバージョンを選び、動いている meltool.exe を置き換える。
package selfupdate

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/otnc/meltool/internal/catalog"
	"github.com/otnc/meltool/internal/versions"
)

// Repository は、Meltool のリリースを置くリポジトリ。
const Repository = "otnc/Meltool"

const exeName = "meltool.exe"

// platformSuffixes は、OS と CPU ごとの配布物の名前の末尾。本家の配布物の名前に合わせる。いまは Windows (x64) だけを配る。
var platformSuffixes = map[[2]string]string{
	{"windows", "amd64"}: "windows",
}

// Select は、current より新しいバージョンのうち、この端末の配布物があるものの最大を返す。いまより古いバージョンには下げない。
// includePre が false なら正式版だけから選ぶ。
func Select(releases []catalog.Release, current string, includePre bool, goos, goarch string) (catalog.Release, catalog.Asset, bool) {
	suffix, ok := platformSuffixes[[2]string{goos, goarch}]
	if !ok {
		return catalog.Release{}, catalog.Asset{}, false
	}
	assetOf := map[string]catalog.Asset{}
	byTag := map[string]catalog.Release{}
	var candidates []versions.Candidate
	for _, r := range releases {
		want := fmt.Sprintf("Meltool-%s-%s.zip", strings.TrimPrefix(r.Tag, "v"), suffix)
		for _, a := range r.Assets {
			if a.Name == want {
				assetOf[r.Tag] = a
				byTag[r.Tag] = r
				candidates = append(candidates, versions.Candidate{Tag: r.Tag, Prerelease: r.Prerelease})
			}
		}
	}
	chosen, ok := versions.Newer(candidates, current, includePre)
	if !ok {
		return catalog.Release{}, catalog.Asset{}, false
	}
	return byTag[chosen.Tag], assetOf[chosen.Tag], true
}

// Apply は、照合済みの配布物 (ZIP) の meltool.exe で、exe を置き換える。
// Windows は動いている実行ファイルを消せないが名前は変えられるので、いまの exe を exe.old に退避してから新しいものを置く。
// 新しいものを置けなければ、退避したものを戻す。前の更新で退避したものは、ここで消す。
func Apply(exe, archive string) error {
	dir := filepath.Dir(exe)
	staged, err := os.CreateTemp(dir, exeName+".*.new")
	if err != nil {
		return fmt.Errorf("新しい meltool.exe を置く場所を作れませんでした: %w", err)
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	err = extractExe(archive, staged)
	if closeErr := staged.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}

	old := exe + ".old"
	if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("前の更新で退避した %s を消せませんでした: %w", old, err)
	}
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("いまの meltool.exe を退避できませんでした: %w", err)
	}
	if err := os.Rename(stagedPath, exe); err != nil {
		if undoErr := os.Rename(old, exe); undoErr != nil {
			return fmt.Errorf("新しい meltool.exe を置けず、元にも戻せませんでした。%s を %s に名前を変えてください: %w", old, exe, errors.Join(err, undoErr))
		}
		return fmt.Errorf("新しい meltool.exe を置けなかったので、元に戻しました: %w", err)
	}
	return nil
}

// Cleanup は、前の更新で退避した meltool.exe を消す。動いている間は消せないので、次に Meltool を使ったときに呼ぶ。消せなくても困らない。
func Cleanup(exe string) {
	os.Remove(exe + ".old")
}

func extractExe(archive string, w io.Writer) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("配布物を開けませんでした: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name != exeName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		// 配布物は数十 MB なので、それを大きく超えるものは異常とみなす
		const maxBytes = 256 << 20
		n, err := io.Copy(w, io.LimitReader(rc, maxBytes+1))
		if err != nil {
			return err
		}
		if n > maxBytes {
			return errors.New("配布物の meltool.exe が大きすぎます")
		}
		return nil
	}
	return fmt.Errorf("配布物に %s がありません", exeName)
}
