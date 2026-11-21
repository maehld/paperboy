#!/bin/sh
# Render the documentation pictures: the example pages and every widget of the
# shipped plugins. The script starts a mock of every upstream service, so it
# needs no credentials and no network. See docs/development.md.
set -eu
cd "$(dirname "$0")/.."
exec go run ./scripts/screenshots/main.go ./scripts/screenshots/mock.go "$@"
