# usagebat v0.5.0

This release adds Codex banked-reset expiry tracking and an English/Japanese interface.

## Highlights

- Shows the authoritative number of available Codex banked resets and the earliest known expiry
- Sends native expiration notifications once at seven days and 24 hours before expiry
- Uses persistent per-reset deduplication without storing raw reset IDs
- Supports system-default, English, and Japanese tray UI and notification text
- Uses the usagebat app identity and icon for native macOS and Windows notifications
- Never consumes a banked reset or creates a Codex session while checking or notifying

## Downloads

- `usagebat_0.5.0_macOS_universal.zip` — Apple Silicon and Intel Mac
- `usagebat_0.5.0_windows_amd64.zip` — standard Intel/AMD Windows PCs
- `usagebat_0.5.0_windows_arm64.zip` — Windows on ARM

The binaries are not Developer ID notarized or Authenticode signed. See the README for first-launch instructions and use `SHA256SUMS.txt` to verify downloads.
