#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
source_svg="$root/assets/usagebat.svg"
iconset="$root/build/usagebat.iconset"

mkdir -p "$root/assets" "$root/build"
rm -rf "$iconset"
mkdir -p "$iconset"

cd "$root"
go run ./scripts/icon-generator -root "$root"

iconutil -c icns "$iconset" -o "$root/packaging/usagebat.icns"
go run github.com/tc-hib/go-winres@v0.3.3 make \
  --in cmd/usagebat/winres/winres.json \
  --arch amd64,arm64 \
  --out cmd/usagebat/rsrc

echo "generated app icons for macOS and Windows from $source_svg"
