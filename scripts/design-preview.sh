#!/bin/sh
# Render the gallery of the shared widget design language. See docs/design/07-design-language.md.
set -eu
cd "$(dirname "$0")/.."
exec go run ./scripts/design-preview/main.go "$@"
