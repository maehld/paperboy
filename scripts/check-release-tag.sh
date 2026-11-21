#!/bin/sh
# Checks the shape of a release tag before the build (ADR 0147):
#
#   server-vX.Y.Z[-rc.N]              the server, SemVer
#   plugins-YYYY.MM.DD[.N][-rc.N]     the plugin set, dated
#   firmware-YYYY.MM.DD[.N][-rc.N]    the firmware bundle, dated
#
# A second release of a train on the same day appends .1, .2, ...
# The date uses dots: a hyphen would mark the tag as a prerelease.
#
#   ./scripts/check-release-tag.sh server-v3.30.0
set -eu

tag=${1:?usage: check-release-tag.sh <tag>}
date='([0-9]{4})\.(0[1-9]|1[0-2])\.(0[1-9]|[12][0-9]|3[01])'
suffix='(-(rc|beta)\.[0-9]+)?'

case "$tag" in
server-v*)
	if ! printf '%s\n' "$tag" | grep -Eq '^server-v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
		echo "invalid server tag: $tag (expected server-vX.Y.Z)" >&2
		exit 1
	fi
	;;
plugins-* | firmware-*)
	train=${tag%%-*}
	rest=${tag#*-}
	if ! printf '%s\n' "$rest" | grep -Eq "^${date}(\.[0-9]+)?${suffix}$"; then
		echo "invalid $train tag: $tag (expected $train-YYYY.MM.DD[.N])" >&2
		exit 1
	fi
	;;
*)
	echo "unknown release train: $tag (expected server-v*, plugins-* or firmware-*)" >&2
	exit 1
	;;
esac
