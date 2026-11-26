# usagebat

[English](README.md) | [日本語](README.ja.md)

<img src="assets/usagebat.png" width="160" alt="usagebat pixel-art battery bat icon">

usagebat keeps the remaining Claude Code, Codex, Cursor, and Antigravity (Gemini) limits visible as a small pixel-art battery in the macOS menu bar or Windows system tray.

It shows remaining capacity, not usage: `100%` is full, and the battery drains as you work.

## Screenshots

### macOS

<img src="assets/screenshot_macos.png" width="904" alt="usagebat showing separate Claude Code and Codex limits in the macOS menu bar">

### Windows

<img src="assets/screenshot_windows.png" width="532" alt="usagebat showing a remaining limit in the Windows system tray">

Windows tray icons have a fixed square shape. With the default `stack` layout, one selected limit is shown as a battery with its percentage, while two or more limits switch to stacked horizontal bars without numbers. Exact values remain available in the tooltip and tray menu. Set `icon.windowsLayout` to `single` to always show only the most constrained limit.

## Features

- Shows real 5-hour, weekly, or monthly limits when the service provides them
- Keeps services in separate cells, with independent period settings and icon visibility
- Detects installed CLIs automatically
- Shows reset times and token details in the tray menu
- Offers battery with percentage, battery-only, and percentage-only styles
- Changes from green to yellow below 50%, then red below 20%
- Adapts label and battery colors to light and dark system bars
- Can launch automatically when you sign in
- Supports multiple Claude Code and Codex profiles without mixing their limits
- Shows available Codex banked resets and their earliest known expiry
- Notifies once at 7 days and 24 hours before a banked reset expires
- Follows the OS language or can be fixed to English or Japanese

## Download

Download the appropriate file from the latest GitHub release:

| Platform | Release file |
|---|---|
| macOS, Apple Silicon or Intel | `usagebat_0.6.3_macOS_universal.zip` |
| Windows on an Intel or AMD processor | `usagebat_0.6.3_windows_amd64.zip` |
| Windows on ARM | `usagebat_0.6.3_windows_arm64.zip` |

Most Windows computers need the `amd64` build. Use `arm64` only for a Windows on ARM device.

At least one supported CLI must be installed and signed in:

- [Claude Code](https://code.claude.com/docs),
- [OpenAI Codex](https://developers.openai.com/codex/cli),
- [Cursor CLI](https://cursor.com/cli) (`agent login`), or
- [Antigravity CLI](https://antigravity.google/docs/getting-started?tab=cli) (`agy`)

### macOS

1. Unzip the download.
2. Move `usagebat.app` to `/Applications`.
3. Open it. usagebat has no Dock icon; look for its battery in the menu bar.

The v0.6.3 build is not notarized. If macOS blocks the first launch, Control-click the app, choose **Open**, then confirm. Move the app before enabling automatic startup so the saved path stays valid.

### Windows

1. Unzip the download to a permanent folder.
2. Run `usagebat.exe`.
3. Look for its battery in the system tray. It may initially be inside the hidden-icons menu.

The v0.6.3 executable is not code-signed, so Microsoft Defender SmartScreen may show a warning. Choose **More info** and **Run anyway** only if you downloaded it from this repository's release page.

## Using the tray menu

Click the menu-bar item on macOS, or right-click the tray icon on Windows. The menu lets you:

- inspect every reported limit and its reset time;
- refresh immediately;
- open **Settings…** to choose the icon style, language, and which services and periods the icon draws;
- enable **Launch at startup**;
- enable banked-reset expiration notifications; and
- see the running version and open the project on GitHub.

By default, usagebat selects the shortest limit each service actually reports. `CL5H` is Claude Code's 5-hour limit, `CXWK` is Codex's weekly limit, `CUMO` is Cursor's billing-cycle limit, and `AG5H` is Antigravity's Gemini 5-hour limit.

The icon follows the system bar appearance. `CL` uses terracotta, `CX` teal, `CU` violet, and `AG` blue. All period suffixes (`5H`, `WK`, and `MO`) share a neutral high-contrast color. Battery status colors also switch between light and dark variants so warning yellow and other details remain legible.

If Claude Code or Codex has multiple configured accounts, each account gets its own cell. A configured `short` replaces the generic `CL`/`CX` label, so work and personal profiles can appear as `CW` and `CP`. A single unnamed default profile keeps the compact service label:

```json
"displaySources": ["claude-code", "codex:codex-work-1a2b", "codex:codex-home-3c4d"],
"sources": {
  "claudeCode": {
    "profiles": [
      { "path": "~/.claude-work", "label": "Claude Work", "short": "CW" },
      { "path": "~/.claude-home", "label": "Claude Home", "short": "CH" }
    ]
  },
  "codex": {
    "profiles": [
      { "path": "~/.codex-work", "label": "Work", "short": "W" },
      { "path": "~/.codex-home", "label": "Home", "short": "H" }
    ]
  }
}
```

Cursor has no profile list: its credential store holds one account per machine, so signing in again replaces the previous account rather than adding one.

`short` is what the icon has room for; it replaces the `CL`/`CX` prefix, and the colour still says which service it is. The menu and the charts use `label`.
Profile order is shared by the icon, tray menu, and charts, and can be changed from the Accounts settings tab.

The macOS menu bar grows a cell per account. A Windows tray icon is sixteen dots square, so it draws at most three bars and keeps the most constrained ones; set `icon.windowsLayout` to `single` if you would rather always see just one.

## Where the numbers come from

### Codex

usagebat asks the Codex app server for the same live account limit snapshot used by Codex `/status`. These percentages and reset times are reported by the service and are not estimated.

If the installed Codex version cannot provide a live snapshot, usagebat can fall back to the newest rate-limit record in `$CODEX_HOME/sessions`. An expired record is never shown as a current value.

When the Codex app server provides earned rate-limit resets, usagebat shows the authoritative available count and the earliest detailed expiry. Expiration notifications never consume a reset and do not create a Codex session.

### Claude Code

Current Claude Code versions cache service-reported utilization in `~/.claude.json`. usagebat reads the 5-hour and weekly figures, reset times, and any additional limits actually present in that cache. Additional profiles use their own `CLAUDE_CONFIG_DIR`, cache, and `projects` directory.

The cache is used directly while fresh. After 15 minutes, or when a cached reset time has passed, usagebat asks `claude -p "/usage" --output-format json` for a current reading. If that command is temporarily unavailable, a cached bucket whose reset time is still in the future remains visible with its age; a bucket is never carried past its reset time.

It can also ask `claude -p "/usage" --output-format json` directly, which is what **Refresh now** does: the cache is only as fresh as the last time Claude Code itself talked to the service, and a refresh you asked for is worth a live reading.

Every percentage comes from the service. A window it does not report is shown as `?` rather than guessed at, and usagebat does not invent a monthly Claude limit. The per-response token tallies in `~/.claude/projects` are still read, but only to report how many tokens went through each window.

### Cursor

Cursor caches no usage figure on disk and its CLI has no command that prints one, so usagebat asks Cursor's own dashboard API — `GetCurrentPeriodUsage` and `GetPlanInfo` — using the credential `agent login` already stored on this machine. usagebat only reads it: the token is never written to the config file, the history file, or the log, and it is sent nowhere but Cursor's own host.

The CLI's official command is `agent`; `cursor-agent` is an alias for the same binary left by its versioned install directory.

Where that credential lives depends on the platform, because the CLI only reaches for a keychain on macOS:

| Platform | Primary | Fallback |
|---|---|---|
| macOS | login keychain, via `security` | `~/.cursor/auth.json` |
| Windows | `%APPDATA%\Cursor\auth.json` | Credential Manager, via `advapi32` |
| Linux | `$XDG_CONFIG_HOME/cursor/auth.json` (default `~/.config/cursor/auth.json`) | `secret-tool` |

On Windows the Credential Manager entry name — when there is one — is chosen by whichever keychain binding the installed CLI was built against, is undocumented, and does not match the other platforms. usagebat therefore tries the names it knows and then enumerates the store, accepting an entry whose name contains both `cursor` and `token` or `access`. Both halves are required so that a saved git password for a repository with `cursor` in its URL is never mistaken for the token and sent to Cursor.

A CLI installed under WSL keeps its credential inside WSL, where a Windows build of usagebat cannot reach it at all. An API key is the way in that does not depend on finding it:

```json
"sources": { "cursor": { "apiKey": "key_..." } }
```

It is deliberately not on the settings page: a token belongs in the config file, not in a field on a local web server.

**No account means no requests.** Cursor appears in the menu only once you are signed in, judged by two independent signals: a credential found in either place above, or the `authId` marker the CLI writes to `~/.cursor/cli-config.json` (that file holds no secret). The macOS keychain check asks only whether the item exists and never reads its secret, so it cannot raise a permission prompt on a machine that does not use Cursor. Either signal is enough — if the token turns out to be unreadable, the menu says so on a row you can act on rather than dropping the service silently. Set `sources.cursor.enabled` to `false` to switch the source off entirely.

Cursor accounts per billing cycle and reports nothing shorter, so the figure lands on the monthly window with `billingCycleEnd` as its reset time. The percentage is the API's own `totalPercentUsed`, which is what the Cursor CLI shows. It is deliberately never reconstructed from `includedSpend` and `limit`: those are a different scale — a live account reading 2.9% carried an amount ratio of 50% — so a response without the percentage is reported as having no figure rather than being approximated into a wrong one.

Pay-as-you-go spending sits outside the included quota and cannot be folded into the same battery, so it is shown in the menu heading as `on-demand $0.00 / $50.00`.

Requests are throttled to one per `sources.cursor.minIntervalSeconds` (30 by default), which **Refresh now** bypasses. If a request fails, the last good reading stays visible, marked as such, for up to `staleAfterSeconds` (900 by default) before being discarded.

### Antigravity (Gemini)

Install and sign in to `agy`, then launch usagebat. It reads Gemini's reported 5-hour and weekly quotas and reset times from `agy -p /usage --output-format json` when the CLI provides them. Authentication stays with the CLI. The tray normally uses the latest reading for up to five minutes, and **Refresh now** runs a fresh query immediately. `sources.antigravity.enabled` controls collection.

## Configuration

Choose **Settings…** from the tray menu to edit the usual display, refresh,
notification, history, update, color, and Claude Code/Codex account settings. Changes are
validated, saved, and applied without restarting the app.

The underlying configuration file is created on first launch:

- macOS: `~/Library/Application Support/usagebat/config.json`
- Windows: `%AppData%\usagebat\config.json`

**Open config file…** remains available for advanced provider and CLI tuning.
Saved manual changes are also reloaded without restarting the app.

Common settings include:

| Setting | Purpose |
|---|---|
| `language` | `auto`, `en`, or `ja` |
| `displayMode` | `both`, `battery`, or `percent` |
| `displaySources` | Services included in the icon |
| `displayLimits.claude-code` | Claude Code's automatic or explicit periods |
| `displayLimits.codex` | Codex's automatic or explicit periods |
| `displayLimits.cursor` | Cursor's automatic or explicit periods |
| `displayLimits.antigravity` | Antigravity Gemini's automatic or explicit periods |
| `refreshSeconds` | Refresh interval; 60 seconds by default |
| `icon.dotSize` | macOS menu-bar artwork size |
| `icon.windowsLayout` | `stack` or `single` on Windows |
| `colors.light` / `colors.dark` | Theme-specific battery, service-label, and period-label colors |
| `colors.warnBelow` / `colors.criticalBelow` | Remaining-percentage warning thresholds |
| `notifications.bankedResetExpiry` | Enable expiry alerts and set threshold hours |
| `history` | Local chart sampling interval and retention |
| `updateCheck` | Optional GitHub release checks and interval |
| `sources.claudeCode.usageCommand.path` | Explicit Claude executable path, if auto-detection fails |
| `sources.claudeCode.profiles` | Claude Code accounts to track, with their `CLAUDE_CONFIG_DIR` and display names |
| `sources.codex.path` | Explicit Codex executable path, if auto-detection fails |
| `sources.codex.profiles` | Codex accounts to track, with the names they are shown under |
| `sources.cursor.enabled` | Whether to read Cursor at all |
| `sources.cursor.apiKey` | Token to use instead of the OS credential store |
| `sources.cursor.minIntervalSeconds` | Shortest gap between dashboard requests |
| `sources.antigravity.enabled` | Whether to read Antigravity Gemini quotas |
| `notifications.limitThresholds` | Warn when headroom drops past a percentage |

The default theme palettes can be customized independently. Changes are picked up while usagebat is running:

```json
"colors": {
  "light": {
    "good": "#15803D", "warn": "#A16207", "critical": "#BE123C",
    "unknown": "#52525B", "claude": "#A94F32", "codex": "#087567",
    "cursor": "#6D28D9", "antigravity": "#174EA6", "period": "#25272B", "textOnFill": "#F8FAFC"
  },
  "dark": {
    "good": "#4ADE80", "warn": "#FACC15", "critical": "#FB7185",
    "unknown": "#A1A1AA", "claude": "#E58A68", "codex": "#52C7B8",
    "cursor": "#A78BFA", "antigravity": "#8AB4F8", "period": "#F2F2F2", "textOnFill": "#101010"
  },
  "warnBelow": 50,
  "criticalBelow": 20
}
```

### Banked-reset notifications

Notifications are enabled by default at seven days and 24 hours before the earliest known expiry:

```json
"notifications": {
  "bankedResetExpiry": {
    "enabled": true,
    "thresholdHours": [168, 24]
  }
}
```

Each reset and threshold is notified only once. Deduplication state is stored separately in `state.json`; changing languages does not resend an alert. macOS uses the app bundle icon and native User Notifications. Windows uses a native WinRT toast registered under the `usagebat` AppUserModelID and does not invoke PowerShell.

The Windows toast schema can only point at an icon file on disk, so usagebat writes `usagebat-toast-icon.png` next to `usagebat.exe`; deleting the program directory removes it too. If that directory is read-only, the icon goes to `%AppData%\usagebat\` instead.

### Multiple profiles

For Claude Code, `"auto"` uses `$CLAUDE_CONFIG_DIR`, or `~/.claude` when unset. For Codex it uses `$CODEX_HOME`, or `~/.codex`. Additional profiles can be configured in the Accounts settings tab or in JSON:

```json
"codex": {
  "enabled": true,
  "path": "",
  "timeoutSeconds": 15,
  "profiles": [
    { "path": "~/.codex-work", "label": "Work", "short": "W" },
    { "path": "~/.codex-personal", "label": "Personal", "short": "P" }
  ]
}
```

Each home is shown as a separate account. usagebat deliberately does not scan arbitrary `.codex-*` directories because that could display a different account without the user selecting it.

Cursor is not part of this: its credential store holds a single account, so there is nothing to enumerate.

## Automatic startup

Enable **Launch at startup** in the tray menu. It creates a per-user LaunchAgent on macOS or a per-user Run entry on Windows, so administrator access is not required.

The registration points to the app's current location. If you move the app or executable later, disable and re-enable the option.

## Privacy

usagebat has no analytics service and does not upload your transcripts or credentials. It reads local CLI state, asks the installed Codex CLI for account limit data, and — only when a Cursor credential exists — asks Cursor's dashboard API for your own plan usage. That Cursor token is read from the OS credential store, sent to Cursor's host alone, and never written to disk by usagebat. Authentication remains managed by Claude Code, Codex, and Cursor. Notification state stores only a shortened hash of the profile, reset ID, and expiry—not the raw reset ID.

## Troubleshooting

Use the diagnostic dump to print the data usagebat currently sees and write the rendered icon:

```sh
# macOS or a source build
./usagebat -dump icon.png

# Windows
usagebat.exe -dump icon.ico
```

If a CLI is installed but absent from the menu, set its absolute executable path in the configuration file. Menu-bar apps often inherit a smaller `PATH` than an interactive terminal.

Banked-reset notifications only fire when a credit is genuinely near expiry, and each one is sent once. To see one on demand, send a test notification through the same path the real alert uses:

```sh
# Windows — also prints the toast registration Windows draws the icon from
usagebat.exe -notify-test

# macOS — notifications are delivered only from the installed app bundle
/Applications/usagebat.app/Contents/MacOS/usagebat -notify-test
```

## Building from source

Go 1.25 or newer is required.

### Install with Go

You can install the command directly from the tagged source:

```sh
go install github.com/yutat23/usagebat/cmd/usagebat@v0.6.3
```

The binary is normally written to `~/go/bin` on macOS or `%USERPROFILE%\go\bin` on Windows. This route is intended for developers:

- macOS requires Xcode Command Line Tools. To add the app identity and icon required by native notifications, install and launch a local app bundle after `go install`:

  ```sh
  usagebat install-app
  ```

  This creates or safely updates `~/Applications/usagebat.app`. Run the command again after upgrading with `go install`. The standalone binary still supports tray display and usage refresh, but not native User Notifications.
- Windows builds made by plain `go install` are not linked as a GUI subsystem application, so a console window may appear.
- Tagged `go install` builds derive their version from Go module metadata.

Running `usagebat` from an interactive terminal starts the tray app in the background and returns to the prompt. Use `usagebat --foreground` when you want the process to stay attached for debugging.

For the normal desktop experience, use the prebuilt release archive instead.

### Local build

```sh
make test
make bundle   # macOS app bundle
make windows  # Windows AMD64 executable
make icons    # regenerate macOS and Windows app-icon resources
```

See [DESIGN.md](DESIGN.md) for the data model, provider behavior, and platform implementation details.

## License

[MIT](LICENSE) © 2026 yutat23
