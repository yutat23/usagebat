usagebat now adapts its pixel-art colors to light and dark menu bars and taskbars.

### What changed

- Added automatic light/dark appearance detection on macOS and Windows
- Colored `CL` and `CX` independently while using one high-contrast color for `5H`, `WK`, and `MO`
- Added theme-specific battery colors so warnings remain readable against either system-bar background
- Added independently customizable `colors.light` and `colors.dark` configuration palettes
- Migrates existing configuration files to version 5 automatically
- Fixed periodic Command Prompt windows appearing during background refreshes on Windows

### Downloads

- `usagebat_0.4.0_macOS_universal.zip` — Apple Silicon and Intel Mac
- `usagebat_0.4.0_windows_amd64.zip` — standard Intel/AMD Windows PCs
- `usagebat_0.4.0_windows_arm64.zip` — Windows on ARM

The binaries are not Developer ID notarized or Authenticode signed. See the README for first-launch instructions and use `SHA256SUMS.txt` to verify downloads.
