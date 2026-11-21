#!/bin/sh
# Fails when a plugin changed without a new version (ADR 0075): every plugin
# carries plugins/cmd/<plugin>/VERSION, and a change to its code must raise it,
# because the plugin zip names the program <plugin>-v<version> and a download
# only installs a higher one.
#
# sdk/plugin and sdk/render are linked into every plugin, so a change there
# changes every program and every version has to rise: a release with unchanged
# version names keeps the old programs and no installation receives the new
# build (ADR 0107). Other shared code under sdk/, the plugin module's go.mod and
# go.sum only warn: whether each affected plugin needs a new version is a
# decision for the maintainer.
#
#   ./scripts/check-plugin-versions.sh origin/main
set -eu

base=${1:-origin/main}
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
status=0

# changed reports whether path differs between the merge base of base and HEAD
# and HEAD. A path that base does not carry counts as changed.
changed() {
    if ! git cat-file -e "$base:$1" 2>/dev/null; then
        return 0
    fi
    ! git diff --quiet "$base"...HEAD -- "$1"
}

shared=0
if changed sdk/plugin || changed sdk/render; then
    shared=1
fi

for dir in plugins/cmd/*/; do
    plugin=${dir#plugins/cmd/}
    plugin=${plugin%/}
    [ -f "$dir/VERSION" ] || continue
    own=0
    if changed "plugins/cmd/$plugin"; then
        own=1
    fi
    [ "$own" -eq 1 ] || [ "$shared" -eq 1 ] || continue
    # The module split moved the versions from cmd/<plugin> to
    # plugins/cmd/<plugin>; the old location is the base before that change.
    old=$(git show "$base:plugins/cmd/$plugin/VERSION" 2>/dev/null | tr -d '[:space:]' || true)
    if [ -z "$old" ]; then
        old=$(git show "$base:cmd/$plugin/VERSION" 2>/dev/null | tr -d '[:space:]' || true)
    fi
    new=$(tr -d '[:space:]' < "$dir/VERSION")
    if [ "$new" = "$old" ]; then
        if [ "$own" -eq 1 ]; then
            echo "::error file=plugins/cmd/$plugin/VERSION::$plugin changed, but its version is still $new; raise plugins/cmd/$plugin/VERSION (ADR 0075)"
        else
            echo "::error file=plugins/cmd/$plugin/VERSION::$plugin is unchanged, but sdk/plugin or sdk/render changed, which alters every plugin binary; raise plugins/cmd/$plugin/VERSION (ADR 0107)"
        fi
        status=1
    else
        echo "$plugin: ${old:-new} -> $new"
    fi
done

if [ "$shared" -eq 0 ] && { changed sdk || changed plugins/go.mod || changed plugins/go.sum; }; then
    echo "::warning::shared code changed (sdk/, plugins/go.mod or plugins/go.sum): check whether the affected plugins need a new VERSION (ADR 0075)"
fi

# The embed directive must sit in each command's package, so every
# plugins/cmd/<plugin>/version.go is a copy. This check keeps the copies identical.
reference=plugins/cmd/calendar/version.go
for file in plugins/cmd/*/version.go; do
    [ "$file" = "$reference" ] && continue
    if ! cmp -s "$reference" "$file"; then
        echo "::error file=$file::$file differs from $reference; keep the plugin version files identical"
        status=1
    fi
done

exit $status
