#!/bin/sh
# Builds the shipped plugins for GOOS/GOARCH (default: the host) and packs
# them into <out>/paperboy-plugins-<os>-<arch>.zip. Unpacked in an instance
# directory, the zip creates plugins/ with the binaries (ADR 0049).
#
#   GOOS=linux GOARCH=arm64 ./scripts/package-plugins.sh dist
set -eu

out=${1:-dist}
goos=${GOOS:-$(go env GOOS)}
goarch=${GOARCH:-$(go env GOARCH)}
ext=""
[ "$goos" = windows ] && ext=".exe"
name="paperboy-plugins-$goos-$goarch"
root=$(cd "$(dirname "$0")/.." && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

mkdir -p "$stage/plugins" "$out"
for app in calendar weather evcc pokedex xkcd; do
  echo "building $app for $goos/$goarch"
  (cd "$root" && CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
    go build -trimpath -ldflags="-s -w" -o "$stage/plugins/$app$ext" "./cmd/$app")
done
cp "$root/cmd/weather/icons/README.md" "$stage/plugins/weather-icons-LICENSE.md"

out=$(cd "$out" && pwd)
rm -f "$out/$name.zip"
(cd "$stage" && zip -qr "$out/$name.zip" plugins)
echo "$out/$name.zip"
