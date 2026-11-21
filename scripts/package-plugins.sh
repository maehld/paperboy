#!/bin/sh
# Builds the shipped plugins for GOOS/GOARCH (default: the host) and packs
# them into <out>/paperboy-plugins-<os>-<arch>.zip. Unpacked in an instance
# directory, the zip creates plugins/ with the binaries (ADR 0049).
# release.yml attaches the zip to a release (ADR 0056).
#
# Every program is named <plugin>-v<version>, the version read from
# plugins/cmd/<plugin>/VERSION (ADR 0075), so a plugin that did not change keeps its
# name across releases and a download only installs a higher one.
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
for app in calendar weather fuel evcc pokedex xkcd departures myskoda countdown rss dexcom github prusalink; do
  version=$(tr -d '[:space:]' < "$root/plugins/cmd/$app/VERSION")
  if [ -z "$version" ]; then
    echo "no version in plugins/cmd/$app/VERSION" >&2
    exit 1
  fi
  echo "building $app $version for $goos/$goarch"
  (cd "$root" && CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
    go build -trimpath -ldflags="-s -w" -o "$stage/plugins/$app-v$version$ext" "./plugins/cmd/$app")
done
cp "$root/plugins/cmd/weather/icons/README.md" "$stage/plugins/weather-icons-LICENSE.md"

out=$(cd "$out" && pwd)
rm -f "$out/$name.zip"
(cd "$stage" && zip -qr "$out/$name.zip" plugins)
echo "$out/$name.zip"
