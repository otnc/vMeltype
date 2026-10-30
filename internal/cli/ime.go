package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/otnc/vmeltype/internal/lifecycle"
	"github.com/otnc/vmeltype/internal/winime"
)

var (
	// errIMECanceled は、IME の DLL の入れ替えで、管理者権限の確認 (UAC) を断られたことを表す。
	errIMECanceled = errors.New("Meltype IME の DLL の入れ替えに要る管理者権限の確認が断られました")
	// errIMEFailed は、IME の DLL を登録できなかったことを表す。本家の登録の処理は、失敗すると前の DLL に戻す。
	errIMEFailed = errors.New("Meltype IME の DLL を登録できませんでした。ほかのアプリが DLL を使っていないかを確かめてください")
)

// matchIME は、実行先を戻したあとや復旧のあとに、登録済みの DLL を実行先にあるバージョンのものに合わせる。
// 実行先を入れ替える前に DLL を入れ替えるので、切り替えが途中で止まると、本体と DLL のバージョンが食い違う。
// 実行先がなければ、合わせる先がないので何もしない。
func matchIME(env *Env, appDir, tag string) error {
	if _, err := os.Stat(appDir); err != nil {
		return nil
	}
	if err := replaceIME(env, appDir, tag); err != nil {
		return fmt.Errorf("Meltype IME の DLL を実行先の %s に合わせられませんでした。\"vmeltype doctor\" で確かめてください: %w", tag, err)
	}
	return nil
}

// fromLabel は、切り替え元を文言に出すための名前を返す。管理前の本家のインストールなら、タグを記録していない。
func fromLabel(from *lifecycle.Activation) string {
	if from == nil {
		return "元の Meltype"
	}
	return from.Tag
}

// replaceIME は、登録済みの Meltype IME の DLL を、切り替え先のバージョンのものに入れ替える。実行先を入れ替える前に呼ぶ。
// 新しいバージョンの本体と古いバージョンの DLL が組み合わさらないよう、入れ替えられなければ切り替えない。
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
