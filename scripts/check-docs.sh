#!/bin/sh
# Checks the house style of the English documents (README, docs/design,
# docs/glossary): a sentence has at most 25 words, and filler words are not
# used. The rules follow ASD-STE100, which this script approximates without its
# dictionary.
#
#   ./scripts/check-docs.sh                 # the design documents and the glossary
#   ./scripts/check-docs.sh docs/design/*.md docs/glossary.md
#
# Tables, headings, code blocks and the "Decisions" lines are not sentences and
# are skipped. A violation prints its file and line and fails the script.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

if [ "$#" -eq 0 ]; then
	set -- docs/design/*.md docs/glossary.md
fi

awk '
BEGIN { fence = 0; para = ""; bad = 0; limit = 25 }

function report(kind, text) {
	bad++
	printf "%s:%d: %s: %s\n", FILENAME, FNR, kind, substr(text, 1, 120)
}

function flush(   s, n, i, arr, count) {
	if (para == "") return
	s = para
	para = ""
	# The "Decisions" paragraph is a list of numbers, not a sentence.
	if (s ~ /^ *ADR [0-9]/) return
	sub(/^ +/, "", s)
	# A link keeps its text, inline code becomes one word, and a decimal point
	# must not split a sentence.
	gsub(/\[([^]]*)\]\([^)]*\)/, "\\1", s)
	gsub(/`[^`]*`/, "code", s)
	gsub(/[0-9]\.[0-9]/, "0x0", s)
	gsub(/[.!?]+[ \t]+/, ".\n", s)
	n = split(s, arr, "\n")
	for (i = 1; i <= n; i++) {
		count = split(arr[i], words, /[ \t]+/)
		if (count > limit) report("sentence of " count " words", arr[i])
	}
}

/^```/ { flush(); fence = !fence; next }
fence { next }
/^[[:space:]]*$/ { flush(); next }
/^#/ { flush(); next }
/^\|/ { flush(); next }
/^[[:space:]]*([-*]|[0-9]+\.)[[:space:]]/ { flush(); para = $0; flush(); next }

{
	line = tolower($0)
	if (line ~ /(^|[^a-z])(simply|just|please|obviously|clearly|basically|actually|easily)([^a-z]|$)/ ||
	    line ~ /of course|in order to|e\.g\.|i\.e\.|etc\./) {
		report("filler word", $0)
	}
	para = para " " $0
}

END {
	flush()
	if (bad > 0) {
		print bad " violation(s)" > "/dev/stderr"
		exit 1
	}
}
' "$@"
