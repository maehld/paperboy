#!/bin/sh
# Fails when a plugin changed without a new version (ADR 0075): every plugin
# carries cmd/<plugin>/VERSION, and a change to its code must raise it, because
# the plugin zip names the program <plugin>-v<version> and a download only
# installs a higher one.
#
# Shared code under pkg/ changes every plugin binary, so it only warns: whether
# each affected plugin needs a new version is a decision for the maintainer.
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

for dir in cmd/*/; do
    plugin=${dir#cmd/}
    plugin=${plugin%/}
    [ -f "$dir/VERSION" ] || continue
    changed "cmd/$plugin" || continue
    old=$(git show "$base:cmd/$plugin/VERSION" 2>/dev/null | tr -d '[:space:]' || true)
    new=$(tr -d '[:space:]' < "$dir/VERSION")
    if [ "$new" = "$old" ]; then
        echo "::error file=cmd/$plugin/VERSION::$plugin changed, but its version is still $new; raise cmd/$plugin/VERSION (ADR 0075)"
        status=1
    else
        echo "$plugin: $old -> $new"
    fi
done

if changed pkg || changed go.mod || changed go.sum; then
    echo "::warning::shared code changed (pkg/, go.mod or go.sum): check whether the affected plugins need a new VERSION (ADR 0075)"
fi

exit $status
