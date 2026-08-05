# usagebat 設計書

## 1. これは何か

Claude Code / Codex の **利用上限（5時間・週次・月次）の残量** を、
macOS のメニューバー / Windows のタスクトレイに **ドット絵のバッテリー** として常駐表示するアプリ。

- 残量 100% = 満充電、使うほど減る（バッテリーの比喩をそのまま採用）
- アイコンに出すサービスと期間をメニューで選択（既定はサービスごとの実在する最短枠）
- クリック（右クリック）でメニューを開き、消費トークン・リセット時刻の内訳を確認
- 表示スタイルは「バッテリー＋%重ね」「バッテリーのみ」「%のみ」から選択

## 2. 用語

| 用語 | 意味 |
|---|---|
| ソース (source) | 計測対象のツール。`claude-code` / `codex` |
| ウィンドウ (window) | 上限の集計期間。`5h` / `weekly` / `monthly` |
| used% | そのウィンドウで消費済みの割合 |
| remaining% | `100 - used%`。バッテリーに表示するのはこちら |
| 加重トークン | モデル係数・種別係数を掛けて正規化したトークン量（表示用の参考値） |

## 3. データ取得方式（重要）

**2つのソースで取得可能な情報の質が根本的に違う。** ここが本アプリ最大の設計上の制約。

### 3.1 Codex — 実測値が取れる

通常は Codex CLI の app server に `account/rateLimits/read` を要求し、CLIの
`/status` と同じライブスナップショットを取得する。`usedPercent`、
`windowDurationMins`、`resetsAt` をそのまま共通モデルへ写す。

互換フォールバックとして、Codex CLI がセッション記録
(`$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl`) の
`event_msg` イベントに、サーバーが返した **実際のレート制限情報** を書き出している。

```jsonc
{"timestamp":"2026-08-02T05:12:17.199Z","type":"event_msg","payload":{
  "type":"token_count",
  "info":{"total_token_usage":{"input_tokens":61097,"cached_input_tokens":49408,
          "output_tokens":402,"reasoning_output_tokens":119,"total_tokens":61499},
          "last_token_usage":{...},"model_context_window":258400},
  "rate_limits":{
    "limit_id":"codex","primary":{"used_percent":0.0,"window_minutes":43800,"resets_at":1788229667},
    "secondary":null,"credits":{"has_credits":false,"unlimited":false,"balance":null},
    "plan_type":"team"}}}
```

→ どちらも推定不要。ただしログの `resets_at` が過去なら、古い現在値として
表示せず、次のライブ取得を待つ。

`window_minutes` からウィンドウ種別を判定する（プランによって primary/secondary の割当が変わるため、
位置ではなく期間長で判定する）：

| window_minutes | 種別 |
|---|---|
| ≤ 720 (12h) | `5h` |
| ≤ 20160 (14d) | `weekly` |
| それ以上 (43800 ≒ 30.4d 等) | `monthly` |

**複数 CODEX_HOME 対応**: Codex は `CODEX_HOME` を切り替えることで仕事用・個人用のように
プロファイルを分けられる。この場合、**別ホーム = 別アカウント = 別枠の上限**である。

したがって次の方針を取る：

- 既定 `homes: ["auto"]` の `auto` は **`$CODEX_HOME`、無ければ `~/.codex` だけ**を指す。
  ディレクトリを探し回らない。ユーザーが名指ししていないホームを勝手に採用すると、
  **他人（別アカウント）の残量をメニューバーに出す**ことになりかねないため
- 複数使いたい場合は明示的に列挙する: `"homes": ["~/.codex-work", "~/.codex-personal"]`
- **列挙されたホームはそれぞれ独立したソースとして扱う。** 1つに畳んで「最も新しい記録を採用」
  としてしまうと、直近に使った方のアカウントの数字が他方の名前で出てしまう
- ホーム名は `~/.codex` なら `Codex`、それ以外は `Codex (~/.codex-work)` のようにパス付きで表示する
- `auto` で何も見つからず、かつ `~/.codex-*` が実在する場合は、
  「`~/.codex-work` を `sources.codex.homes` に追加してください」とメニューに出す。
  黙って拾うのではなく、ユーザーに決めさせる

### 3.2 Claude Code — ローカル利用量キャッシュから実測値を取る

現在のClaude Codeは `~/.claude.json` の `cachedUsageUtilization` に、サービスから取得した
実際の利用率をキャッシュする：

```jsonc
"cachedUsageUtilization": {
  "fetchedAtMs": 1785654163063,
  "utilization": {
    "five_hour": { "utilization": 71, "resets_at": "2026-08-02T10:19:59Z" },
    "seven_day": { "utilization": 8, "resets_at": "2026-08-05T23:59:59Z" }
  }
}
```

互換フォールバックの特性：

| 項目 | 実測値 |
|---|---|
| トークン消費 | **0**（`num_turns: 0` / `total_cost_usd: 0` / `duration_api_ms: 0`。LLM ターンが発生しない） |
| 所要時間 | 約 1.2〜1.3 秒 |
| 副作用 | セッションファイルは蓄積しない（連続実行してもファイル数が増えないことを確認済み） |

キャッシュを一次データ源とし、`staleAfterSeconds` より古ければ採用しない。

古いCLIとの互換性のため `claude -p /usage --output-format json` のパーサーも残す。
現在のCLIはこの呼び出しに契約枠を返さないため、認識不能なら値なしとして扱う。

`sources.claudeCode.usageCommand` で設定：

```jsonc
"usageCommand": {
  "enabled": true,
  "path": "",                  // 空 = 自動検出
  "timeoutSeconds": 20,
  "minIntervalSeconds": 30,    // refreshSeconds と独立したスロットル
  "staleAfterSeconds": 900     // 直前の成功値を再利用してよい上限
}
```

**バイナリの解決**: メニューバーアプリを Finder から起動すると PATH が最小限になり
`~/.local/bin` が入らない。そのため `exec.LookPath` に加えて
`~/.local/bin/claude` / `~/.claude/local/claude` / `/opt/homebrew/bin/claude` /
`/usr/local/bin/claude`（Windows は `.exe`）を明示的に探索する。

**行のパース**: `<ラベル>: <N>% used · resets <日時>` を正規表現で拾い、
ラベルに `session` を含めば 5h、`week` なら weekly、`month` なら monthly に対応付ける。
同じウィンドウに複数バケット（例: `Current week (all models)` と `Current week (Opus)`）が
出た場合は**使用率が高い方**を採用する（実際に効く制約はそちら）。

**認識できない行は捨てる。** 出力フォーマットが変わったら「データなし」に落として
値なしとして扱う。読めない文字列から数字をひねり出して間違った残量を出すより良い。

**スロットルと劣化**: `minIntervalSeconds` 未満の再実行はスキップする（メニューの
「Refresh now」を連打してもプロセスが増えない）。実行に失敗した場合は
`staleAfterSeconds` 以内なら直前の成功値を再利用し、それを超えたら破棄して値なしとする。

### 3.3 Claude Code — 報告されない枠は推定しない

実測キャッシュは通常 5h と weekly を返す（プランによる）。標準の月次枠はないので、
**monthly をトランスクリプトから作り出さない**。将来キャッシュが実際に monthly グループを
返した場合のみ、実測枠として扱う。

**キャッシュも `/usage` も値を返さない枠は、推定せず `?` を表示する。**

0.6.0 より前は、トランスクリプトの加重トークンを**ユーザーが手で較正した上限値**と
比べて 5h / weekly を推定していた。これは廃止した。理由は2つある。

- 較正値も係数も Anthropic の非公開仕様に対する当て推量であり、実測と大きくずれた
  （実運用で「推定 0% 残 vs 実測 51% 残」という乖離が出た）
- サービスが正確な値を返すようになった今、**ユーザーに数値を較正させてまで
  推定を維持する理由がない**

トランスクリプトの走査自体は続ける。ただし用途は**期間ごとのトークン量の表示**だけで、
利用率の判断には一切使わない。

- 入力: `~/.claude/projects/**/*.jsonl` の `type:"assistant"` 行にある `message.usage`
  （`input_tokens` / `output_tokens` / `cache_creation_input_tokens` / `cache_read_input_tokens`）
  と `timestamp` / `message.model`
- 重複排除: `message.id` + `requestId` の組（セッション再開・サイドチェーンで同一行が複数ファイルに出る）
- 加重トークン: メニューと使用状況グラフに出す参考値
  ```
  weighted = modelWeight(model) × ( in×1 + out×5 + cacheCreate×1.25 + cacheRead×0.1 )
  modelWeight: opus=5, sonnet=1, haiku=0.2, その他=1
  ```

`weeklyMode` / `monthlyMode` は、このトークン集計の期間境界を決めるためだけに残る。

### 3.4 ソースの優先順位

ウィンドウごとに次の順で決める：

1. ユーザーが明示的に更新を要求した場合は `/usage` を直接実行する
2. Claude Codeのローカル利用量キャッシュ
3. `/usage` が返した実測値またはその直前値（`staleAfterSeconds` 以内）
4. どれも無ければ `?`

すべてサービスの報告値であり、推定は存在しない。
トークン実数（in / out / cache / weighted）は 1〜4 のどれでも常に表示する。

### 3.5 ウィンドウ境界の決め方（Claude Code のトークン集計）

| ウィンドウ | 既定 | 備考 |
|---|---|---|
| `5h` | 直近アクティビティから遡る 5h ブロック | ccusage と同じ規則: 最初のエントリを時刻切り捨てしてブロック開始、5h 経過 or 5h 以上の無活動で新ブロック |
| `weekly` | ローリング 7 日 | 実際のリセット曜日はアカウント依存で不明なため。`calendar` + 曜日/時刻指定も可 |

## 4. 表示設計

### 4.1 ドット絵バッテリー

すべての描画を **ドット（論理ピクセル）グリッド** 上で行い、最後に整数倍拡大する。
これにより拡大してもエッジがボケず、ドット絵の質感が保たれる。

バッテリースプライト（17 × 11 ドット）:

```
 0123456789...        ← x
0 ██████████████ ..    本体外枠 15 幅
1 █············█ ..
2 █············█ ..
3 █············█ ██    ← 端子 (2×5)
4 █············█ ██
5 █············█ ██
6 █············█ ██
7 █············█ ██
8 █············█ ..
9 █············█ ..
10██████████████ ..
```
- 内側 13 × 9 ドットが残量ゲージ領域。左から `remaining%` の比率で塗る
- 3×5 のピクセルフォントで `100` は 11 ドット幅 → 内側に左右 1 ドット余白で収まる

### 4.2 レイアウト（macOS: 横ストリップ）

ラベルをバッテリーの**上**に置くことで横幅を抑える。

```
 5H       WK       MO
▐███ 87▌ ▐██ 62▌ ▐█ 41▌
```
- セル = ラベル(3×5フォント2文字=7ドット) を 17 ドット幅の中央に配置
- セル高 = 5(ラベル) + 2(間隔) + 11(バッテリー) = 18 ドット
- アイコン全体 = 17×3 + 3×2(間隔) = 57 ドット幅 × 18 ドット高
- 論理サイズ 1pt/ドット → **57 × 18 pt**（メニューバー 22pt に収まる）、Retina では 2px/ドットで描画

### 4.3 レイアウト（Windows: 正方形スタック）

Windows のタスクトレイアイコンは **正方形固定（16/20/24/32 px）でテキストを添えられない**。
横ストリップは物理的に不可能なので、正方形用レイアウトを別に持つ。

- `stack`（既定）: 16×16 ドットに横バー 3 本（5h / weekly / monthly を上から）を積む。数値は出せないので色と塗り量で示し、ツールチップとメニューで実数を出す
- `single`: 最も残量の少ないウィンドウのバッテリー 1 個 + 2 桁の残量%

ICO は 16/20/24/32 px のマルチサイズで出力し、DPI スケーリングに追従させる。

### 4.4 表示モード（ユーザー選択）

| モード | 内容 |
|---|---|
| `both`（既定） | バッテリー枠 + 塗り + 残量%を重ねる |
| `battery` | バッテリーのみ（数字なし） |
| `percent` | 数字のみ（`87%`）。枠を描かない |

メニューから即時切り替え、設定ファイルに永続化。

### 4.5 配色

メニューバー／タスクバーのライト・ダークをOSから判定し、テーマ別の配色を使う。
サービス略号と期間は役割を分け、`CL`/`CX` はサービス色、`5H`/`WK`/`MO`
はすべて同じ高コントラストな期間色で描く。

| 要素 | ライト | ダーク |
|---|---|---|
| Claude (`CL`) | `#A94F32` | `#E58A68` |
| Codex (`CX`) | `#087567` | `#52C7B8` |
| 期間 (`5H/WK/MO`) | `#25272B` | `#F2F2F2` |
| accent（残量 ≥ 50%） | `#15803D` | `#4ADE80` |
| accent（残量 ≥ 20%） | `#A16207` | `#FACC15` |
| accent（残量 < 20%） | `#BE123C` | `#FB7185` |
| 不明（`?`） | `#52525B` | `#A1A1AA` |
| 数字（塗りの上） | `#F8FAFC` | `#101010` |

しきい値・色はすべて設定可能。

### 4.6 メニュー（クリック / 右クリック）

```
Claude Code  —  reported by /usage      ← 見出し（無効項目）
  5h        51% left  ·  resets in 3h33m (19:19)
      in 229 · out 158.0K · cache 17.7M · weighted 15.2M
  Weekly    94% left  ·  resets in 3d17h (Thu 08:59)
      in 331 · out 228.4K · cache 22.2M · weighted 21.1M
──────────
Codex (~/.codex-work)  —  plan: team    ← ホームごとに独立した見出し
  Monthly  100% left  ·  resets Aug 30 12:47
      latest session: in 61.1K · out 402 · cache 49.4K
──────────
表示 ▸  ● バッテリー + %
        ○ バッテリーのみ
        ○ % のみ
ウィンドウ ▸ ☑ 5H  ☑ WK  ☑ MO
今すぐ更新
設定ファイルを開く
終了
```

## 5. 実装方針

### 5.1 言語

実装言語は **Go**。理由:
- 単一バイナリで配布でき、常駐アプリとしてランタイム依存がない
- macOS は cgo で Objective-C を直接叩ける／Windows は syscall のみで完結
- JSONL の逐次パースが速く、メモリフットプリントが小さい（常駐前提で重要）

UI文字列は英語と日本語を持ち、`language` が `auto` の場合はOSの優先言語を使う。
設定値、プロトコル名、プロバイダから返る生の診断文は互換性と調査のしやすさを優先して英語のままとする。

### 5.2 トレイ実装

**プラットフォームごとに別バックエンド**とし、共通インタフェース `tray.Backend` で抽象化する。

- **macOS: 自前 cgo + Objective-C**（`NSStatusItem` / `NSMenu`）
  既製の `fyne.io/systray` は内部で `[image setSize:NSMakeSize(16,16)]` と
  **16×16pt 固定**にしてしまい、横長ストリップが潰れる。4.2 のレイアウトが成立しないため自前実装する。
  UI 操作は必ずメインスレッドで行う。メインスレッド上の `NSTimer` から Go 側の
  tick 関数を呼び、そこで最新スナップショットを反映する（ロック競合と
  `dispatch_async` の非同期性を排除するため）。
- **Windows: `fyne.io/systray`**
  正方形アイコンしか許されないプラットフォームなので上記の制約が問題にならず、
  `Shell_NotifyIcon` 周りを自前実装する利益がない。

### 5.3 更新ループ

- 既定 60 秒間隔（設定可）でプロバイダを実行 → `Snapshot` を生成 → アトミックに差し替え
- ファイル読み込みは **追記分のみの増分読み** （path → offset/size/mtime をキャッシュ）。
  トランスクリプトは追記専用なので、常駐中の CPU コストはほぼゼロになる
- 走査対象は `monthly` のルックバック期間 + 余裕（既定 35 日）以内に更新されたファイルのみ

### 5.4 パッケージ構成

```
cmd/usagebat/main.go        エントリポイント（メインスレッド確保、更新ループ、メニュー配線）
internal/model/                  Snapshot / SourceStatus / WindowStatus など共通型
internal/config/                 設定の読み書き・既定値・保存先解決
internal/appbundle/              go install版からmacOS .appを安全に生成
internal/i18n/                   OS言語判定・英語／日本語カタログ
internal/notify/                 banked reset期限判定・永続的な重複防止
internal/provider/               Provider インタフェース
internal/provider/claudecode/    利用量キャッシュ・/usage・トークン集計
internal/provider/codex/         Codexライブ制限 + rollout jsonlフォールバック
internal/render/font.go          3×5 ピクセルフォント
internal/render/battery.go       スプライト描画 / ストリップ・スタックレイアウト
internal/render/ico.go           Windows 用 ICO エンコーダ（マルチサイズ）
internal/tray/                   Backend インタフェース + darwin(cgo) / windows 実装
```

### 5.5 設定ファイル

保存先は `os.UserConfigDir()` 基準:
- macOS: `~/Library/Application Support/usagebat/config.json`
- Windows: `%AppData%\usagebat\config.json`

初回起動時に既定値で自動生成する。

通知済み状態は同じディレクトリの `state.json` に分離して保存する。reset IDそのものは保存せず、
プロファイル・reset ID・期限から作った短いハッシュと通知済み閾値だけを保持する。

### 5.6 banked reset期限通知

- Codex app-serverの `account/rateLimits/read` が返す所持数を正とし、明細で分かる最短期限を対象にする
- 既定閾値は168時間（7日）と24時間。reset・閾値ごとに一度だけ通知する
- 複数閾値を過ぎた状態で起動した場合は最も緊急な通知を1件だけ出し、該当済み閾値をまとめて記録する
- 通知は案内のみでbanked resetを消費せず、Codexセッションも作成しない
- macOSはUser Notifications、WindowsはWinRT Toastを直接使い、シェルやPowerShellを起動しない
- 明細または期限を取得できないフォールバック状態では、誤通知を避けるため通知しない

## 6. 設定スキーマ（既定値）

```jsonc
{
  "version": 6,
  "language": "auto",              // auto | en | ja
  "displayMode": "both",           // both | battery | percent
  "displaySources": ["claude-code", "codex"],
  "displayLimits": {
    "claude-code": { "autoShortest": true, "windows": ["5h"] },
    "codex": { "autoShortest": true, "windows": ["5h"] }
  },
  "refreshSeconds": 60,
  "icon": {
    "dotSize": 1.2,                // 1 ドットあたりの pt（macOS）
    "pixelScale": 2,               // 描画時の px/ドット
    "windowsLayout": "stack"       // stack | single
  },
  "colors": {
    "light": {
      "good": "#15803D", "warn": "#A16207", "critical": "#BE123C",
      "unknown": "#52525B", "claude": "#A94F32", "codex": "#087567",
      "period": "#25272B", "textOnFill": "#F8FAFC"
    },
    "dark": {
      "good": "#4ADE80", "warn": "#FACC15", "critical": "#FB7185",
      "unknown": "#A1A1AA", "claude": "#E58A68", "codex": "#52C7B8",
      "period": "#F2F2F2", "textOnFill": "#101010"
    },
    "warnBelow": 50, "criticalBelow": 20
  },
  "notifications": {
    "bankedResetExpiry": {
      "enabled": true,
      "thresholdHours": [168, 24]
    }
  },
  "sources": {
    "claudeCode": {
      "enabled": true,
      "projectsDir": "",           // 空 = ~/.claude/projects
      "weeklyMode": "rolling",     // rolling | calendar
      // 旧CLI互換フォールバック。トークン消費ゼロ。
      "usageCommand": { "enabled": true, "path": "", "timeoutSeconds": 20,
                        "minIntervalSeconds": 30, "staleAfterSeconds": 900 },
      "weights": { "output": 5, "cacheCreation": 1.25, "cacheRead": 0.1,
                   "models": { "opus": 5, "sonnet": 1, "haiku": 0.2 } }
    },
    "codex": {
      "enabled": true,
	  "path": "",                 // 空 = Codex CLIを自動検出
	  "timeoutSeconds": 15,
      "homes": ["auto"]            // auto = $CODEX_HOME、無ければ ~/.codex のみ
    }
  }
}
```

## 7. アイコンに出す値の集約

既定の `autoShortest` では、サービスごとに実在する最短枠を1つ選び、別セルで表示する。
たとえばClaudeの5h枠は `CL5H`、Codex Teamの月次枠は `CXMO` となる。
同一サービスに複数アカウントがある場合だけ、同じ期間の最小残量を集約する。
サービスは Claude Code のみ / Codex のみ / 両方から選べる。
固定期間も `displayLimits` によりサービスごとに独立し、Claudeを5h、Codexをmonthlyのように
別々に指定できる。
バッテリーとして意味があるのは「あと何ができるか」なので、最も逼迫した制約が正しい。
ソース別の内訳はメニューで見せる。

## 8. スコープ外（v1 では作らない）

- Linux 対応（`tray.Backend` の実装を足せば入る構造にはする）
- 通常の利用枠残量が閾値を割ったときの通知（banked reset期限通知は対象内）
- Claude Code に存在しない月次枠の推定（そもそも推定自体を行わない）
