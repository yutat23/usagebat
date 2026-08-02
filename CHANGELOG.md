# Changelog

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
