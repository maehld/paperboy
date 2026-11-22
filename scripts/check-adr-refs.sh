#!/bin/sh
# Checks the references between the documents. Two kinds exist:
#
# - Go code cites the decision behind a rule as "ADR NNNN", and the documents
#   live under docs/adr/. A reference to a missing number fails the script.
# - A document links another one as [text](path.md). A link to a missing file
#   fails the script, because prose and link can disagree (ADR 0151 was linked
#   as 0150 in docs/plugins/protocol.md, and four links in
#   docs/design/05-plugins.md missed their ../adr/ prefix).
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

# Every markdown link between the documents must resolve. A link with an anchor
# is checked without the anchor; an absolute URL and a link that starts with a
# slash are not files of this repository.
missing=$(mktemp)
trap 'rm -f "$missing"' EXIT
for document in README.md AGENTS.md $(find docs -name '*.md' | sort); do
	dir=$(dirname "$document")
	grep -oE '\]\([^)]+\)' "$document" | sed -e 's/^](//' -e 's/)$//' -e 's/#.*$//' |
		while IFS= read -r link; do
			case "$link" in
			'' | http://* | https://* | mailto:* | /*) continue ;;
			esac
			# -e, because a document may link a directory such as sdk/design.
			if [ ! -e "$dir/$link" ]; then
				echo "$document: the link $link does not resolve" >>"$missing"
			fi
		done
done
if [ -s "$missing" ]; then
	cat "$missing" >&2
	bad=1
fi

if [ "$bad" -ne 0 ]; then
	exit 1
fi
