# Issue の扱い方

Issue と PR は GitHub の [otnc/vMeltype](https://github.com/otnc/vMeltype) で管理し、`gh` コマンドで操作します。

- コミットメッセージや PR の本文に `#123` とあれば、その Issue が作業の仕様
- Issue はテンプレートと同じ項目で書き、タイトルは何をするかが分かる短い日本語にする。開発の作業には `task` ラベルを付ける。作者の判断が要るものには `needs-author` を付ける
- PR の本文の先頭に `Closes #<番号>` を書き、テンプレートの構成で書く。実施していない検証にチェックを付けない
- PR のタイトルと各コミットは、コミットメッセージの決まり (CONTRIBUTING.md) に合わせる
