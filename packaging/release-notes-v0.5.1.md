# usagebat v0.5.1

This maintenance release fixes a crash in standalone macOS binaries and makes native notifications available to `go install` users through a local app bundle.

## Highlights

- Prevents `go install` binaries from crashing when a banked-reset notification becomes due
- Adds `usagebat install-app`, which safely creates or updates `~/Applications/usagebat.app`
- Includes the usagebat Bundle ID, app icon, and ad-hoc signature needed by native macOS notifications
- Keeps an unavailable notification unsent so launching the `.app` can deliver it later
- Adds macOS and Windows screenshots and documents Windows multi-limit icon behavior

## Downloads

- `usagebat_0.5.1_macOS_universal.zip` — Apple Silicon and Intel Mac
- `usagebat_0.5.1_windows_amd64.zip` — standard Intel/AMD Windows PCs
- `usagebat_0.5.1_windows_arm64.zip` — Windows on ARM

The binaries are not Developer ID notarized or Authenticode signed. See the README for first-launch instructions and use `SHA256SUMS.txt` to verify downloads.
