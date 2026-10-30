# vMeltype

vMeltype は、雪解けのように半角/全角の壁を溶かす日本語入力 [Meltype](https://github.com/yksr-melt/Meltype) の、バージョン管理をはじめとした外付けの多機能ツールです。CLI は `vmeltype` です。

複数のバージョンを保持し、正式版、プレリリース、古いバージョンを選んで切り替えます。ほかに、データのスナップショットと別の PC への持ち運び、中断した処理の復旧、問題の診断もできます。

> [!WARNING]
> いまはプレリリース (0.1.0-pre) で、Windows (x64) だけに対応しています。まだない機能があり、動きや保存の形式が変わることがあります。

## 取得する

[Releases](https://github.com/otnc/vMeltype/releases) から `vMeltype-<バージョン>-windows.zip` を取得し、展開した `vmeltype.exe` を PATH の通った場所に置きます。

vMeltype はコード署名をしていないので、初めて実行するときに Windows の SmartScreen の警告が出ることがあります。代わりに、このリポジトリの GitHub Actions が作ったものかを、次のどちらかで確かめられます。

```powershell
# GitHub の証明 (artifact attestation) を確かめる。GitHub CLI が要る
gh attestation verify vMeltype-0.1.0-pre.1-windows.zip -R otnc/vMeltype

# リリースに置いた .sha256 と比べる
Get-FileHash vMeltype-0.1.0-pre.1-windows.zip -Algorithm SHA256
```

## 使い方

```powershell
vmeltype releases           # 公式のリリースの一覧を表示する
vmeltype install 1.4.0      # 取得して保持する。有効なバージョンは変えない
vmeltype use 1.4.0          # 保持したバージョンに切り替える。通信しない
vmeltype check              # 新しいバージョンがあるかを確かめる
vmeltype update             # 新しいバージョンを取得して切り替える (upgrade でも同じ)
vmeltype rollback           # 直前のバージョンに戻す
vmeltype pin                # いまのバージョンから切り替えないよう固定する
vmeltype snapshot create    # Meltype のデータを保存する
vmeltype snapshot export <id> data.vmeltype-snapshot  # 保存したデータを 1 つのファイルにする。別の PC で import できる
vmeltype status             # 有効なバージョン、配置、データ先を表示する
vmeltype doctor             # 問題がないかを調べ、直し方を示す
vmeltype app update         # vMeltype 自身を新しいバージョンにする (self upgrade でも同じ)
```

`--pre` を付けるとプレリリース、`--portable` を付けると portable 版を対象にします。各コマンドの説明は `vmeltype <コマンド> --help` で表示します。

通常版の切り替えでは、本家と同じ場所 (`%LOCALAPPDATA%\Programs\Meltype` と `%LOCALAPPDATA%\Meltype`) を使い、次のことを行います。

- 動いている Meltype を正常に終了させ、切り替えたあとに起動し直す
- 切り替えの前に、データのスナップショットを作る
- 本家の自動更新を止める (`config.json` の `AutoUpdate` を `false` にする)
- Meltype IME を登録済みなら、IME の DLL も切り替え先のバージョンのものに入れ替える。DLL が違えば、管理者権限の確認 (UAC) が出る

設定、保持バージョン、スナップショットは管理ルート (既定は `%LOCALAPPDATA%\vMeltype`) に置きます。

## vMeltype をやめる

```powershell
vmeltype app uninstall      # self uninstall でも同じ
```

管理ルートを消します。Meltype も削除するか (既定は残す) と、スナップショットも削除するか (既定は消す) を聞きます。

- Meltype を残すなら、いまのバージョンをそのまま使える状態で残し、本家の自動更新を管理前の値に戻します
- Meltype も削除するなら、本家のアンインストーラーを使います。本家の Install.cmd や vMeltype だけで入れた Meltype は、設定と学習データも消えます

いま動いている `vmeltype.exe` は消せないので、終わったあとに手で消してください。

## 開発

開発への参加の手順は [CONTRIBUTING.md](CONTRIBUTING.md)、用語は [CONTEXT.md](CONTEXT.md) にあります。

## ライセンス

[GNU General Public License v3.0](LICENSE)
