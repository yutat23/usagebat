# Changelog

## Unreleased

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
