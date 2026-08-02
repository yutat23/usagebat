# usage-battery

Claude Code と Codex が実際に持つ利用上限（5時間 / 週次 / 月次）の**残量**を、
macOS のメニューバー・Windows のタスクトレイに**ドット絵のバッテリー**で常駐表示します。

```
 5H
▐87%▌
```

- 残量 100% = 満充電。使うほど減ります
- 50% 未満で黄、20% 未満で赤
- クリック / 右クリックでメニューが開き、消費トークンとリセット時刻の内訳が見られます
- 表示は「バッテリー + %」「バッテリーのみ」「% のみ」から選択
- アイコンに含めるサービス（Claude Code / Codex / 両方）と制限期間を選択可能
- CLIがインストールされていないサービスはアイコンとメニューから自動的に省略
- メニューの「Launch at startup」でログイン時の自動起動を切り替え
- 既定はサービスごとに、実際に存在する最短枠を1つずつ表示

設計の詳細と判断の根拠は [DESIGN.md](DESIGN.md) にあります。

## ビルドと起動

Go 1.21 以降が必要です（開発は 1.25 で行っています）。

```sh
# macOS
make bundle
open build/UsageBattery.app

# 直接動かす場合
make run

# Windows 向けバイナリ（macOS からクロスビルド可）
make windows
```

ログイン時の自動起動は、トレイメニューの「Launch at startup」をチェックしてください。
macOSではユーザーLaunchAgent、WindowsではユーザーRunエントリへ登録されるため、
管理者権限は不要です。アプリを移動する場合は、移動後に一度チェックし直してください。

## 設定

初回起動時に設定ファイルが自動生成されます。

- macOS: `~/Library/Application Support/usage-battery/config.json`
- Windows: `%AppData%\usage-battery\config.json`

メニューの「Open config file…」から開けます。**保存すると再起動なしで反映されます。**

主な項目：

| キー | 意味 |
|---|---|
| `version` | 設定スキーマのバージョン（自動管理） |
| `displayMode` | `both` / `battery` / `percent` |
| `displaySources` | アイコンに含めるサービス。`["claude-code","codex"]` |
| `displayLimits.claude-code` | Claudeの最短自動・固定期間を設定 |
| `displayLimits.codex` | Codexの最短自動・固定期間を設定 |
| `refreshSeconds` | 更新間隔（既定 60 秒） |
| `icon.dotSize` | 1 ドットあたりの pt。既定1.2（macOS） |
| `icon.windowsLayout` | `stack`（3本バー）/ `single`（バッテリー1個）（Windows） |
| `colors` | 配色としきい値 |
| `sources.claudeCode.usageCacheFile` | 空なら `~/.claude.json` の実測キャッシュを読む |
| `sources.claudeCode.usageCommand` | `/usage` ポーリングの設定（`path` を空にすると自動検出） |
| `sources.claudeCode.limits` | `/usage` が返さないウィンドウの推定用（下記） |
| `sources.codex.path` | Codex CLI。空なら `PATH` と標準的な配置から自動検出 |
| `sources.codex.homes` | `["auto"]` で `$CODEX_HOME`、無ければ `~/.codex` |

## データの出どころ（重要）

**2つのソースで取得できる情報の質が違います。**

### Codex — 実測値

本アプリは Codex CLI の app server に問い合わせ、`/status` と同じアカウントの
レート制限情報をライブ取得します。使用率・期間・リセット時刻はいずれも
Codexサービスの実測値で、**推定は入りません**。

CLIが古い、または一時的に応答しない場合のみ、セッションログ
（`$CODEX_HOME/sessions/**/rollout-*.jsonl`）の最新値へフォールバックします。
リセット時刻を過ぎたログは現在値として表示しません。

既定 (`homes: ["auto"]`) が見るのは **`$CODEX_HOME`、無ければ `~/.codex` だけ**です。

仕事用と個人用のように `CODEX_HOME` を切り替えて使っている場合は、明示的に列挙してください。

```json
"codex": { "enabled": true, "path": "", "timeoutSeconds": 15,
           "homes": ["~/.codex-work", "~/.codex-personal"] }
```

**別ホームは別アカウント＝別枠の上限**なので、列挙したホームはそれぞれ独立した項目として
メニューに並びます（1つに畳んで混ぜることはしません）。
アイコンには、表示対象として選んだソース中で最も残量の少ない値が出ます。

ディレクトリを勝手に探し回らないのは意図的です。ユーザーが名指ししていないホームを
拾ってしまうと、別アカウントの残量をメニューバーに出すことになりかねないためです。
`~/.codex-*` が実在するのに `auto` で見つからなかった場合は、
「これを `homes` に追加してください」とメニューに出します。

### Claude Code — ローカル利用量キャッシュの実測値（＋足りない分だけ推定）

現在のClaude Codeは、サービスから取得した利用率を `~/.claude.json` の
`cachedUsageUtilization` に保存します。本アプリは `five_hour` / `seven_day` とリセット時刻を
ここから読みます。キャッシュなので、既定では15分を超えて古い値は実測値として使いません。

古いClaude Codeとの互換性のため `claude -p "/usage" --output-format json` も
フォールバックとして残しています。現在のCLIではこのコマンドが契約枠ではなくヘッドレス実行自身の
コストを返すため、認識できない出力は安全に捨てて推定へ移ります。

Finder から起動するとアプリの PATH が最小限になるため、`claude` は
`~/.local/bin` / `~/.claude/local` / Homebrew / `/usr/local/bin` も明示的に探します。
見つからない場合は `sources.claudeCode.usageCommand.path` に絶対パスを書いてください。

実測キャッシュが返すのは通常 5h と weekly です（プランによる）。標準の月次枠はないため、
**monthly は推定で作りません**。将来キャッシュが月次枠を実際に返した場合だけ表示します。
キャッシュが無い・古い場合、設定済みの 5h / weekly 枠は推定にフォールバックします。

```
加重トークン = モデル係数 × ( 入力×1 + 出力×5 + キャッシュ作成×1.25 + キャッシュ読込×0.1 )
モデル係数: opus=5, sonnet=1, haiku=0.2
残量% = 100 − 加重トークン / limits[window] × 100
```

> **推定値には `(est)` が付きます。** 係数も上限値も非公開仕様なので実測とはズレます。
> 実際、既定値のままだと 5h の推定は実測 51% 残に対して 0% 残とかなり外れました。
> 実測キャッシュが新鮮な限りそちらが常に優先されるので、通常は気にする必要はありません。

**較正のしかた**：メニューに加重トークンの実数が出ています。
実測キャッシュの値と突き合わせて `limits` を調整してください。
`0` にすると推定をやめ、そのウィンドウは `?` 表示になります。

ウィンドウの区切り方も設定できます（推定時のみ影響します）：

| ウィンドウ | 既定 | 備考 |
|---|---|---|
| `5h` | 直近の 5h ブロック | 最初の発話を時刻切り捨てして開始、5h 経過か 5h 無活動で次のブロック |
| `weekly` | ローリング 7 日 | `weeklyMode: "calendar"` で暦週 |

## アイコンに出る値

既定ではサービスごとに実在する最短期間を選び、Claudeは `CL5H`、Codexは `CXMO` のように
別々のセルで表示します。同じサービスに複数アカウントがある場合だけ、同じ期間のうち
最も残量が少ない値を採用します。
サービスと期間はメニューでいつでも切り替えられ、ソース別の全内訳もメニューで確認できます。
期間指定はClaudeとCodexで独立しており、たとえばClaudeだけ5h、Codexだけmonthlyにできます。

## プラットフォーム差

- **macOS**: メニューバーは横長画像を許すので、選択した期間を横に並べます
- **Windows**: タスクトレイは正方形固定でテキストを添えられないため、
  1項目なら領域いっぱいのバッテリー、複数なら領域を使い切るバーを積みます。
  16〜64pxのDPI別アイコンを内蔵し、Windows側で縮小されにくくしています。
  数値はツールチップとメニューで確認してください

## トラブルシュート

現在読めているデータをそのまま出力します。

```sh
./build/usage-battery -dump /tmp/icon.png
```

メニューに出るはずの内容が標準出力に、アイコンが `/tmp/icon.png` に書かれます。

## 開発

```sh
make test        # 全テスト
make vet
USAGE_BATTERY_PREVIEW_DIR=/tmp/preview go test ./internal/render/ -run Preview
```

最後のコマンドは各表示モード・各残量のアイコンを PNG に書き出し、
明背景・暗背景に合成したコンタクトシート（`sheet-light.png` / `sheet-dark.png`）も作ります。
