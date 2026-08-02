# usagebat

[English](README.md) | [日本語](README.ja.md)

<img src="assets/usagebat.png" width="160" alt="usagebatのピクセルアート電池コウモリアイコン">

usagebatは、Claude CodeとCodexの残り利用枠を、macOSのメニューバーまたはWindowsのシステムトレイに小さなピクセルアート電池として表示します。

表示するのは使用率ではなく残量です。`100%`が満タンで、利用するほど電池が減っていきます。

## 主な機能

- サービスが提供する5時間・週間・月間の実測値を表示
- Claude CodeとCodexを分け、期間を個別に選択
- 未インストールのCLIを自動的に非表示
- リセット時刻とトークン詳細をメニューに表示
- ライト／ダークのシステムバーに合わせて配色を変更
- Codexのbanked reset所持数と最短期限を表示
- banked resetの期限7日前と24時間前に一度ずつ通知
- OS言語への自動追従、英語固定、日本語固定に対応
- ログイン時の自動起動と複数Codexプロファイルに対応

## ダウンロード

[最新のGitHub Release](https://github.com/yutat23/usagebat/releases/latest)から環境に合うファイルをダウンロードしてください。

| 環境 | ファイル |
|---|---|
| macOS（Apple Silicon / Intel） | `usagebat_0.5.0_macOS_universal.zip` |
| Windows Intel / AMD | `usagebat_0.5.0_windows_amd64.zip` |
| Windows on ARM | `usagebat_0.5.0_windows_arm64.zip` |

Claude CodeまたはCodexの少なくとも一方がインストールされ、ログイン済みである必要があります。

### macOS

1. ZIPを展開します。
2. `usagebat.app`を`/Applications`へ移動します。
3. アプリを開き、メニューバーの電池を確認します。Dockには表示されません。

現在の配布物は公証されていません。初回起動をブロックされた場合は、Controlキーを押しながらアプリをクリックして「開く」を選択してください。

### Windows

1. ZIPを常設するフォルダーへ展開します。
2. `usagebat.exe`を実行します。
3. システムトレイの電池を確認します。最初は隠れているアイコン内に入る場合があります。

現在の配布物はコード署名されていません。Microsoft Defender SmartScreenが表示された場合は、このリポジトリのReleaseから取得したファイルであることを確認してから実行してください。

## メニュー

macOSではメニューバー項目をクリック、Windowsではトレイアイコンを右クリックします。メニューから次を変更できます。

- アイコンの表示形式
- 表示するサービスと期間
- banked reset期限通知のON/OFF
- システム言語／英語／日本語
- OS起動時の自動起動
- 今すぐ更新、設定ファイルを開く、終了

既定では、各サービスが実際に報告する最短の制限を選択します。`CL5H`はClaude Codeの5時間枠、`CXWK`はCodexの週間枠、`CXMO`はCodexの月間枠です。

## 値の取得元

### Codex

Codex CLIのapp-serverへ`account/rateLimits/read`を要求し、Codex `/status`と同じライブ利用枠を取得します。利用率、リセット時刻、banked reset所持数と明細はサービス報告値で、推定ではありません。

ライブ取得に対応していないCodexでは、`$CODEX_HOME/sessions`内の最新記録へフォールバックします。ただし、フォールバックではbanked reset情報を取得できません。

### Claude Code

現在のClaude Codeが`~/.claude.json`へ保存する利用率キャッシュから、5時間・週間枠とリセット時刻を取得します。互換用に`claude -p "/usage" --output-format json`も試し、どちらも利用できない場合のみローカル履歴から推定します。推定値には「（推定）」と表示します。

## banked resetの期限通知

既定では、期限7日前と24時間前に一度ずつ通知します。

```json
"notifications": {
  "bankedResetExpiry": {
    "enabled": true,
    "thresholdHours": [168, 24]
  }
}
```

通知はresetを消費せず、Codexセッションも作成しません。複数の通知段階を過ぎた状態で起動した場合は、最も緊急な通知を1件だけ表示します。macOSではusagebatアプリのアイコン、WindowsではusagebatのAppUserModelIDと埋め込みアイコンを使用します。

## 設定

設定ファイルは初回起動時に作成されます。

- macOS：`~/Library/Application Support/usagebat/config.json`
- Windows：`%AppData%\usagebat\config.json`

メニューの「設定ファイルを開く…」から編集できます。保存した変更は再起動せず反映されます。

主な設定：

| 設定 | 内容 |
|---|---|
| `language` | `auto`、`en`、`ja` |
| `displayMode` | `both`、`battery`、`percent` |
| `displaySources` | アイコンに表示するサービス |
| `displayLimits` | サービスごとの期間選択 |
| `refreshSeconds` | 更新間隔。既定60秒、最小5秒 |
| `colors.light` / `colors.dark` | テーマ別配色 |
| `notifications.bankedResetExpiry` | 期限通知と通知タイミング |
| `sources.codex.homes` | Codexプロファイルの場所 |

## 自動起動

メニューの「OS起動時に自動起動」を有効にしてください。管理者権限は不要です。アプリを移動した場合は、一度無効にしてから再度有効にしてください。

## プライバシー

usagebatには分析サービスがなく、履歴や認証情報を外部へ送信しません。認証はClaude CodeとCodexが管理します。通知の重複防止状態には、プロファイル・reset ID・期限から作った短いハッシュだけを保存し、生のreset IDは保存しません。

## ソースからのビルド

Go 1.25以降が必要です。

```sh
go install github.com/yutat23/usagebat/cmd/usagebat@v0.5.0
```

通常のデスクトップ利用には、macOSの`.app`やWindows GUIサブシステム設定を含むRelease版を推奨します。

```sh
make test
make bundle
make windows
```

## ライセンス

[MIT](LICENSE) © 2026 yutat23
