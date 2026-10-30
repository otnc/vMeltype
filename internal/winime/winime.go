// Package winime は、本家の Meltype IME の DLL を、切り替え先のバージョンのものに入れ替える。
// 入れ替えは、切り替え先のバージョンに入っている本家の meltype-ime.ps1 の関数に任せる。本家は、DLL を管理者だけが書き換えられる場所に写して
// hash を確かめてから、管理者として登録する。入力方式の一覧と既定の入力方式は変えない。
package winime

// Result は、入れ替えの結果。
type Result string

const (
	// ResultCurrent は、登録済みの DLL が切り替え先のバージョンと同じで、何もしなかったこと。
	ResultCurrent Result = "current"
	// ResultReplaced は、DLL を入れ替えて登録し直したこと。
	ResultReplaced Result = "replaced"
	// ResultCanceled は、管理者権限の確認 (UAC) で断られたこと。
	ResultCanceled Result = "cancelled"
	// ResultFailed は、管理者として動いた登録が失敗したこと。
	ResultFailed Result = "failed"
	// ResultCorrupt は、切り替え先のバージョンの DLL が、ビルドしたときのものと違うこと。
	ResultCorrupt Result = "corrupt"
	// ResultUnsupported は、切り替え先のバージョンに、本家の入れ替えの仕組みがないこと (v1.0.x など)。
	ResultUnsupported Result = "unsupported"
	// ResultARM64 は、ARM64 の Windows で、本家が IME に対応していないこと。
	ResultARM64 Result = "arm64"
)
