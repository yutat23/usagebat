usagebat now has its own pixel-art battery-bat app icon and behaves naturally when launched from a terminal.

### What changed

- Added the new usagebat app icon to macOS bundles and Windows AMD64/ARM64 executables
- Running `usagebat` interactively now starts the tray app in the background and returns to the prompt
- Added `usagebat --foreground` for attached debugging and logs
- Added reproducible icon generation from the checked-in pixel grid

### Downloads

- `usagebat_0.3.0_macOS_universal.zip` — Apple Silicon and Intel Mac
- `usagebat_0.3.0_windows_amd64.zip` — standard Intel/AMD Windows PCs
- `usagebat_0.3.0_windows_arm64.zip` — Windows on ARM

The binaries are not Developer ID notarized or Authenticode signed. See the README for first-launch instructions and use `SHA256SUMS.txt` to verify downloads.
