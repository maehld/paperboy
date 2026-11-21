#!/bin/sh
# Checks that every ADR reference in Go code resolves to a document. Go code
# cites the decision behind a rule as "ADR NNNN"; the documents live under
# docs/adr/. A reference to a missing number fails the script.
#
#   ./scripts/check-adr-refs.sh
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

bad=0
for number in $(grep -rhoE 'ADR[s]? [0-9]{4}' --include='*.go' server plugins sdk |
	awk '{print $NF}' | sort -u); do
	if ! ls docs/adr/"$number"-*.md >/dev/null 2>&1; then
		echo "ADR $number is referenced in Go code but docs/adr/$number-*.md is missing" >&2
		bad=1
	fi
done

if [ "$bad" -ne 0 ]; then
	exit 1
fi
