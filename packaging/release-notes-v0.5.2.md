# usagebat v0.5.2

This maintenance release fixes Windows notifications arriving without an icon, and adds a way to look at a notification without waiting for one to become due.

## Highlights

- Puts the usagebat icon on Windows toast notifications, which previously arrived blank
- Outlines that icon in white so it stays readable on the dark notification surface
- Writes the toast icon next to `usagebat.exe`, so deleting the program directory removes it
- Shows the running version in the menu, with a link to the project on GitHub
- Adds `usagebat -notify-test`, which sends one notification through the real path and prints the platform state behind it
- Restores console output for `-dump`, `-notify-test`, and `-version` on Windows, where the tray binary is linked as a GUI application
- Corrects the English banked-reset notification, which read "1 banked reset expire"

## Downloads

- `usagebat_0.5.2_macOS_universal.zip` — Apple Silicon and Intel Mac
- `usagebat_0.5.2_windows_amd64.zip` — standard Intel/AMD Windows PCs
- `usagebat_0.5.2_windows_arm64.zip` — Windows on ARM

The binaries are not Developer ID notarized or Authenticode signed. See the README for first-launch instructions and use `SHA256SUMS.txt` to verify downloads.
