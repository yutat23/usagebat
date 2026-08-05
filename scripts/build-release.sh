#!/bin/sh
set -eu

version=${1:-}
case "$version" in
  ''|*[!0-9.]*|.*|*.)
    echo "usage: $0 VERSION (for example: 0.3.0)" >&2
    exit 2
    ;;
esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
release_dir="$root/dist/v$version"
if [ -e "$release_dir" ]; then
  echo "release directory already exists: $release_dir" >&2
  exit 1
fi

stage=$(mktemp -d "${TMPDIR:-/tmp}/usagebat-release.XXXXXX")
trap 'rm -rf "$stage"' EXIT HUP INT TERM
mkdir -p "$release_dir" "$stage/usagebat.app/Contents/MacOS" \
  "$stage/usagebat.app/Contents/Resources"

module=github.com/yutat23/usagebat
# -s -w drop the symbol table and DWARF, which is about a third of the binary.
# Stack traces survive; only debugger symbols are lost.
version_flag="-s -w -X ${module}/internal/version.Value=$version"
cache_dir=${GOCACHE:-"${TMPDIR:-/tmp}/usagebat-go-cache"}

cd "$root"
GOCACHE="$cache_dir" go test ./...

MACOSX_DEPLOYMENT_TARGET=11.0 CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
  CC="clang -arch arm64 -mmacosx-version-min=11.0" GOCACHE="$cache_dir" \
  go build -ldflags "$version_flag" -o "$stage/usagebat-darwin-arm64" ./cmd/usagebat
MACOSX_DEPLOYMENT_TARGET=11.0 CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
  CC="clang -arch x86_64 -mmacosx-version-min=11.0" GOCACHE="$cache_dir" \
  go build -ldflags "$version_flag" -o "$stage/usagebat-darwin-amd64" ./cmd/usagebat
lipo -create \
  "$stage/usagebat-darwin-arm64" \
  "$stage/usagebat-darwin-amd64" \
  -output "$stage/usagebat.app/Contents/MacOS/usagebat"
sed "s/@VERSION@/$version/g" packaging/Info.plist > "$stage/usagebat.app/Contents/Info.plist"
cp LICENSE "$stage/usagebat.app/Contents/Resources/LICENSE.txt"
cp packaging/usagebat.icns "$stage/usagebat.app/Contents/Resources/usagebat.icns"
codesign --force --deep --sign - "$stage/usagebat.app"
ditto -c -k --keepParent --norsrc "$stage/usagebat.app" \
  "$release_dir/usagebat_${version}_macOS_universal.zip"

CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOCACHE="$cache_dir" \
  go build -ldflags "-H windowsgui $version_flag" \
  -o "$stage/usagebat.exe" ./cmd/usagebat
cp LICENSE "$stage/LICENSE.txt"
(cd "$stage" && zip -q -j \
  "$release_dir/usagebat_${version}_windows_amd64.zip" usagebat.exe LICENSE.txt)

CGO_ENABLED=0 GOOS=windows GOARCH=arm64 GOCACHE="$cache_dir" \
  go build -ldflags "-H windowsgui $version_flag" \
  -o "$stage/usagebat.exe" ./cmd/usagebat
(cd "$stage" && zip -q -j \
  "$release_dir/usagebat_${version}_windows_arm64.zip" usagebat.exe LICENSE.txt)

(cd "$release_dir" && shasum -a 256 ./*.zip > SHA256SUMS.txt)
echo "Release assets written to $release_dir"
