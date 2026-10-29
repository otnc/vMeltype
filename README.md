# vMeltype

vMeltype は、日本語入力の [Meltype](https://github.com/yksr-melt/Meltype) の複数の版を保持し、正式版、プレリリース、旧版を選んで切り替える版の管理ツールです。CLI は `vmeltype` です。

> [!WARNING]
> 開発を始めたところで、まだ動くものはありません。

## 予定している使い方

```sh
vmeltype releases          # 公式のリリースの一覧を取得して表示する
vmeltype install v1.4.0    # 取得して保持する。有効な版は変えない
vmeltype use v1.4.0        # 保持した版に切り替える。通信しない
vmeltype snapshot create   # 本家を止めてデータを保存する
```

## 開発

開発への参加の手順は [CONTRIBUTING.md](CONTRIBUTING.md)、用語は [CONTEXT.md](CONTEXT.md) にあります。

## ライセンス

[GNU General Public License v3.0](LICENSE)
