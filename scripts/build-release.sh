#!/bin/sh
set -eu

version=${1:-}
case "$version" in
  ''|*[!0-9.]*|.*|*.)
    echo "usage: $0 VERSION (for example: 0.1.0)" >&2
    exit 2
    ;;
esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
release_dir="$root/dist/v$version"
if [ -e "$release_dir" ]; then
  echo "release directory already exists: $release_dir" >&2
  exit 1
fi

stage=$(mktemp -d "${TMPDIR:-/tmp}/usage-battery-release.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
mkdir -p "$release_dir" "$stage/UsageBattery.app/Contents/MacOS" \
  "$stage/UsageBattery.app/Contents/Resources"

module=github.com/yutat23/usage-battery
version_flag="-X ${module}/internal/version.Value=$version"
cache_dir=${GOCACHE:-"${TMPDIR:-/tmp}/usage-battery-go-cache"}

cd "$root"
GOCACHE="$cache_dir" go test ./...

MACOSX_DEPLOYMENT_TARGET=11.0 CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
  CC="clang -arch arm64 -mmacosx-version-min=11.0" GOCACHE="$cache_dir" \
  go build -ldflags "$version_flag" -o "$stage/usage-battery-darwin-arm64" ./cmd/usage-battery
MACOSX_DEPLOYMENT_TARGET=11.0 CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
  CC="clang -arch x86_64 -mmacosx-version-min=11.0" GOCACHE="$cache_dir" \
  go build -ldflags "$version_flag" -o "$stage/usage-battery-darwin-amd64" ./cmd/usage-battery
lipo -create \
  "$stage/usage-battery-darwin-arm64" \
  "$stage/usage-battery-darwin-amd64" \
  -output "$stage/UsageBattery.app/Contents/MacOS/usage-battery"
sed "s/@VERSION@/$version/g" packaging/Info.plist > "$stage/UsageBattery.app/Contents/Info.plist"
cp LICENSE "$stage/UsageBattery.app/Contents/Resources/LICENSE.txt"
codesign --force --deep --sign - "$stage/UsageBattery.app"
ditto -c -k --keepParent --norsrc "$stage/UsageBattery.app" \
  "$release_dir/UsageBattery_${version}_macOS_universal.zip"

CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOCACHE="$cache_dir" \
  go build -ldflags "-H windowsgui $version_flag" \
  -o "$stage/usage-battery.exe" ./cmd/usage-battery
cp LICENSE "$stage/LICENSE.txt"
(cd "$stage" && zip -q -j \
  "$release_dir/usage-battery_${version}_windows_amd64.zip" usage-battery.exe LICENSE.txt)

CGO_ENABLED=0 GOOS=windows GOARCH=arm64 GOCACHE="$cache_dir" \
  go build -ldflags "-H windowsgui $version_flag" \
  -o "$stage/usage-battery.exe" ./cmd/usage-battery
(cd "$stage" && zip -q -j \
  "$release_dir/usage-battery_${version}_windows_arm64.zip" usage-battery.exe LICENSE.txt)

(cd "$release_dir" && shasum -a 256 ./*.zip > SHA256SUMS.txt)
echo "Release assets written to $release_dir"
