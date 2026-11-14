#!/bin/sh
# Fails when a plugin changed without a new version (ADR 0075): every plugin
# carries cmd/<plugin>/VERSION, and a change to its code must raise it, because
# the plugin zip names the program <plugin>-v<version> and a download only
# installs a higher one.
#
# pkg/plugin and pkg/render are linked into every plugin, so a change there
# changes every program and every version has to rise: a release with unchanged
# version names keeps the old programs and no installation receives the new
# build (ADR 0107). Other shared code under pkg/, go.mod and go.sum only warn:
# whether each affected plugin needs a new version is a decision for the
# maintainer.
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
if changed pkg/plugin || changed pkg/render; then
    shared=1
fi

for dir in cmd/*/; do
    plugin=${dir#cmd/}
    plugin=${plugin%/}
    [ -f "$dir/VERSION" ] || continue
    own=0
    if changed "cmd/$plugin"; then
        own=1
    fi
    [ "$own" -eq 1 ] || [ "$shared" -eq 1 ] || continue
    old=$(git show "$base:cmd/$plugin/VERSION" 2>/dev/null | tr -d '[:space:]' || true)
    new=$(tr -d '[:space:]' < "$dir/VERSION")
    if [ "$new" = "$old" ]; then
        if [ "$own" -eq 1 ]; then
            echo "::error file=cmd/$plugin/VERSION::$plugin changed, but its version is still $new; raise cmd/$plugin/VERSION (ADR 0075)"
        else
            echo "::error file=cmd/$plugin/VERSION::$plugin is unchanged, but pkg/plugin or pkg/render changed, which alters every plugin binary; raise cmd/$plugin/VERSION (ADR 0107)"
        fi
        status=1
    else
        echo "$plugin: $old -> $new"
    fi
done

if [ "$shared" -eq 0 ] && { changed pkg || changed go.mod || changed go.sum; }; then
    echo "::warning::shared code changed (pkg/, go.mod or go.sum): check whether the affected plugins need a new VERSION (ADR 0075)"
fi

exit $status
