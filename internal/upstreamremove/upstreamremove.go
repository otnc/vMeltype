// Package upstreamremove は、本家のアンインストーラーを呼んで、通常版の Meltype を削除する。
// 本家の入れ方ごとに、本家が用意したアンインストーラーを使う。IME の登録、自動起動のタスク、ショートカット、「設定 → アプリ」の登録も、本家が外す。
package upstreamremove

// Method は、使ったアンインストーラー。
type Method string

const (
	// MethodInno は、本家の setup.exe (Inno Setup) が置いた unins000.exe。設定と学習データを消すかは、本家が聞く。
	MethodInno Method = "inno"
	// MethodScript は、本家の Install.cmd が置いた uninstall.ps1。設定と学習データも消す。
	MethodScript Method = "script"
	// MethodFallbackScript は、どちらもないとき (vMeltype だけで入れたとき) に使う、保持バージョンのうち有効なものの uninstall.ps1。設定と学習データも消す。
	MethodFallbackScript Method = "fallback-script"
)
