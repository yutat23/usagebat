# Changelog

## Unreleased

- Fixed chart hover readouts for unevenly spaced token records and closely spaced quota samples, and kept small heatmap peaks visible in the legend.

## 0.6.2 — 2026-08-13

### Added

- Charts now name the point under the cursor: the date, time, and reading, so a dip in the line can be read without guessing at the axis
- Labelled the activity heatmap as percentage points of the limit used in that hour, with a 0%–peak% colour ramp, so the cells are not a unitless scale

### Fixed

- Fixed the settings-page mascot vanishing in dark mode: its casing is the same near-black as the page, so the dark theme now gets the cream outline the Windows toast already uses

### Changed

- Showed a format example in every settings text field, so clearing a comma-separated list or a path does not leave the reader without the shape of a valid value
- Moved the icon pickers — which services and periods to draw, the icon style, and the UI language — out of the tray menu and onto the settings page, so the menu stays a reading of remaining quota rather than a second copy of those checkboxes

## 0.6.1 — 2026-08-09

### Added

- Added Cursor as a third source, drawn as a `CU` cell in a Cursor violet, with its own service toggle, period selection, and palette entry. Cursor accounts per billing cycle and reports nothing shorter, so its figure lands on the monthly window
- Read the Cursor figure from Cursor's dashboard API, using the credential `agent login` already stored on this machine — the login keychain on macOS, `auth.json` on Windows (`%APPDATA%\Cursor`) and Linux (`$XDG_CONFIG_HOME/cursor`), with a Credential Manager lookup behind it on Windows. The token is only ever read, is never written to the config file, history or log, and is sent nowhere but Cursor's own host
- Showed Cursor's pay-as-you-go spending in the menu heading, since it sits outside the included quota and cannot share the battery
- Added `sources.cursor.apiKey` for machines whose stored credential cannot be reached, such as a CLI installed under WSL, and `sources.cursor.enabled` to switch the source off entirely

### Changed

- Migrated existing configurations to schema 9, which adds Cursor to the icon rather than leaving a new source collected but invisible

### Fixed

- Fixed a low-limit warning being sent again for the window it had already covered. Its deduplication key carried the reset time, which the providers do not report identically between readings — Claude's usage cache gives RFC 3339 seconds where `/usage` gives whole minutes — so every manual refresh, and every cache expiry, looked like a fresh window and repeated the warning. The key is now the source and window alone, with the window recorded beside it and a tolerance for readings that disagree by seconds. Upgrading resets that record, so each watched limit may warn once more than it otherwise would
- Fixed two notifications being sent for one limit when it reached the icon twice, since nothing is recorded until each event is delivered

### Notes

- Cursor appears only once you are signed in, judged by a stored credential or by the `authId` marker in `~/.cursor/cli-config.json`, which holds no secret. The macOS keychain check asks only whether the item exists, so a machine that does not use Cursor makes no request and sees no permission prompt
- Cursor's percentage is the API's own `totalPercentUsed`, and is never rebuilt from `includedSpend` and `limit`: those are a different scale, and a live account reading 2.9% carried an amount ratio of 50%
- The Cursor CLI's official command is `agent`; `cursor-agent` is an alias for the same binary

## 0.6.0 — 2026-08-09

### Added

- Added a settings screen served to the browser from a guarded loopback origin, covering the display, refresh, notification, history, update, colour and account settings, so the usual ones no longer need the config file
- Added usage charts — headroom over time, token usage, and a weekday-by-hour heatmap — drawn from history recorded locally and pruned automatically
- Added an optional check for new releases on GitHub, off by default, which sends nothing about the user
- Added warnings when a watched limit runs low, at 50% and again at 20% of remaining headroom. Only the limits the icon is already drawing are watched, because choosing what the icon shows is how the user says which limits they care about
- Added independently named profiles for Claude Code and Codex: a `label` for menus, charts and notifications, and a `short` the icon draws in place of the `CL`/`CX` prefix, so accounts stop being identified by a hash of a directory
- Added the mascot to the settings page, wearing the expression the tightest limit deserves

### Changed

- Reported only what the service says about Claude Code limits. The estimate built from transcript tokens against a hand-calibrated ceiling is gone: it disagreed with the real figures badly enough to be worse than saying nothing, so an unreported window now shows `?`. Transcripts are still read, but only for the token tallies
- Refreshed usage after the machine wakes. Figures from before the lid closed are certainly wrong at exactly the moment somebody looks at them, and a timer that was itself suspended cannot report that it was — but the wall clock says so plainly
- Applied a setting without reloading the settings page, so changing one checkbox no longer redraws every chart and throws away the scroll position. A browser that will not run the script gets the forms exactly as they were
- Drew one icon cell per account on macOS. A Windows tray icon is sixteen dots square, so it draws the three most constrained and stops; that is a property of the space, not of the implementation
- Migrated Codex's list of home directories into named profiles on load

## 0.5.2 — 2026-08-04

### Added

- Added an about section to the menu showing the running version, with a link to the project on GitHub
- Added `usagebat -notify-test`, which sends one notification through the same path and wording the banked-reset alert uses and prints the platform state behind it
- Added console output for one-shot runs on Windows, where the tray binary is linked as a GUI application and `-dump`, `-notify-test`, and `-version` previously printed nowhere

### Changed

- Moved the Windows toast icon next to the executable, so deleting the program directory removes it; a read-only install directory still falls back to `%AppData%\usagebat\`
- Outlined the Windows toast icon in white so its near-black casing stays readable on the dark notification surface

### Fixed

- Fixed Windows notifications arriving without an icon: the toast payload now carries an `appLogoOverride` image, which is what Windows actually draws, rather than relying on the AppUserModelId registration alone
- Fixed English banked-reset notifications reading "1 banked reset expire"

## 0.5.1 — 2026-08-03

### Added

- Added `usagebat install-app` to turn a macOS `go install` binary into `~/Applications/usagebat.app` with native notification support
- Added macOS and Windows screenshots and documented the Windows multi-limit icon layout

### Fixed

- Prevented standalone macOS binaries installed with `go install` from crashing when a banked-reset notification becomes due

## 0.5.0 — 2026-08-03

### Added

- Added Codex banked-reset counts and earliest-expiry details from the live app-server response
- Added native expiration notifications at seven days and 24 hours, with persistent deduplication
- Added system-default, English, and Japanese tray UI and notification text
- Added a Japanese README

### Changed

- Migrated configuration to version 6 with language and notification settings

## 0.4.0 — 2026-08-02

### Added

- Added light- and dark-system-bar palettes on macOS and Windows
- Added separate Claude (`CL`), Codex (`CX`), and shared period-label colors

### Changed

- Updated battery warning colors for stronger contrast on both light and dark bars
- Migrated configuration to version 5 with independently customizable `colors.light` and `colors.dark` palettes

### Fixed

- Prevented Codex and Claude background refresh subprocesses from flashing Command Prompt windows on Windows

## 0.3.0 — 2026-08-02

### Added

- Added a pixel-art battery-bat app icon to macOS bundles and Windows executables
- Added `--foreground` for terminal-attached debugging

### Changed

- Interactive `usagebat` launches now detach after starting the tray app and return control to the terminal

## 0.2.0 — 2026-08-02

### Changed

- Renamed the product, app, executable, Go module, configuration directory, and startup registration to `usagebat`
- Added tagged module-version detection for `go install`

The GitHub repository moved from `yutat23/usage-battery` to `yutat23/usagebat`. GitHub redirects the old URL, but new installations should use the new module path.

## 0.1.0 — 2026-08-02

First public release.

### Highlights

- Pixel-art limit batteries for Claude Code and Codex on macOS and Windows
- Live Codex limits matching `/status`, with safe rollout-log fallback
- Claude Code usage-cache readings with clearly marked estimation fallback
- Independent service and period selection
- Automatic hiding of CLIs that are not installed
- Per-user launch-at-login support
- Multiple Codex profile support
- Universal macOS build and native Windows AMD64 and ARM64 builds

### Distribution notes

The 0.1.0 binaries are not Developer ID notarized or Authenticode signed. Follow the first-launch instructions in the README and verify downloads with `SHA256SUMS.txt`.
