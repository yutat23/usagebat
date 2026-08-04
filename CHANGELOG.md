# Changelog

## Unreleased

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
