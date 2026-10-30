package cli

import (
	"errors"
	"fmt"

	"github.com/otnc/vmeltype/internal/winime"
)

var (
	// errIMECanceled は、IME の DLL の入れ替えで、管理者権限の確認 (UAC) を断られたことを表す。
	errIMECanceled = errors.New("Meltype IME の DLL の入れ替えに要る管理者権限の確認が断られたので、切り替えませんでした。実行先と IME は変えていません")
	// errIMEFailed は、IME の DLL を登録できなかったことを表す。本家の登録の処理は、失敗すると前の DLL に戻す。
	errIMEFailed = errors.New("Meltype IME の DLL を登録できませんでした。実行先は変えていません。ほかのアプリが DLL を使っていないかを確かめて、もう一度実行してください")
)

// replaceIME は、登録済みの Meltype IME の DLL を、切り替え先の版のものに入れ替える。実行先を入れ替える前に呼ぶ。
// 新しい版の本体と古い版の DLL が組み合わさらないよう、入れ替えられなければ切り替えない。
func replaceIME(env *Env, storedAppDir, tag string) error {
	result, err := env.ReplaceIME(storedAppDir)
	if err != nil {
		return err
	}
	switch result {
	case winime.ResultCurrent, winime.ResultReplaced:
		return nil
	case winime.ResultCanceled:
		return errIMECanceled
	case winime.ResultFailed:
		return errIMEFailed
	case winime.ResultCorrupt:
		return fmt.Errorf("%s の Meltype IME の DLL が、ビルドしたときのものと違います。\"vmeltype install %s --reinstall\" で取得し直してください", tag, tag)
	case winime.ResultARM64:
		return errors.New("ARM64 の Windows では、本家が Meltype IME に対応していないので、IME 登録済みの PC では切り替えません")
	default:
		return fmt.Errorf("%s には本家の Meltype IME の入れ替えの仕組みがないので、Meltype IME を登録済みの PC では切り替えに対応していません", tag)
	}
}
