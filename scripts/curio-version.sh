#!/usr/bin/env bash
# Print BuildVersion from build/version.go (e.g. 1.28.6 or 1.29.0-rc1).
# Same source as curio --version, Debian packages, OpenRPC, and generated CLI docs.
set -euo pipefail

file="${1:-build/version.go}"
if [[ ! -f "$file" ]]; then
	echo "curio-version: $file not found" >&2
	exit 1
fi

array="$(sed -n 's/.*BuildVersionArray = \[3\]int{\([0-9][0-9]*\), *\([0-9][0-9]*\), *\([0-9][0-9]*\)}.*/\1.\2.\3/p' "$file")"
rc="$(sed -n 's/.*BuildVersionRC = \([0-9][0-9]*\).*/\1/p' "$file" | tail -n1)"

if [[ -z "$array" ]]; then
	echo "curio-version: could not parse BuildVersionArray in $file" >&2
	exit 1
fi

if [[ -n "$rc" && "$rc" != 0 ]]; then
	printf '%s-rc%s\n' "$array" "$rc"
else
	printf '%s\n' "$array"
fi
