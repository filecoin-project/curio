#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
context_dir="$repo_root/scripts/ai-context/context"
prompt_dir="$repo_root/scripts/ai-context/prompt"
all_md_dir="$repo_root/scripts/ai-context"

fail=0

err() {
  echo "ERROR: $*" >&2
  fail=1
}

info() {
  echo "INFO: $*"
}

check_context_headings() {
  info "Checking context file headings..."
  while IFS= read -r -d '' file; do
    first_nonempty="$(awk 'NF {print; exit}' "$file")"
    if [[ -z "$first_nonempty" || ! "$first_nonempty" =~ ^#\  ]]; then
      err "Context file must start with a level-1 heading: $file"
    fi
  done < <(find "$context_dir" -maxdepth 1 -type f -name '*.md' -print0 | sort -z)
}

normalize_target() {
  local raw="$1"

  # Drop surrounding whitespace.
  raw="$(echo "$raw" | sed -E 's/^[[:space:]]+|[[:space:]]+$//g')"

  # Skip title part in markdown links if present: path "title"
  raw="$(echo "$raw" | sed -E 's/[[:space:]]+".*"$//')"

  # Remove anchor.
  raw="${raw%%#*}"

  # Remove line suffix of form :123 or :123:45 for local file references.
  raw="$(echo "$raw" | sed -E 's/:[0-9]+(:[0-9]+)?$//')"

  echo "$raw"
}

check_links() {
  info "Checking markdown links in scripts/ai-context..."
  while IFS= read -r -d '' file; do
    while IFS= read -r link; do
      target="$(normalize_target "$link")"

      [[ -z "$target" ]] && continue
      [[ "$target" =~ ^https?:// ]] && continue
      [[ "$target" =~ ^mailto: ]] && continue
      [[ "$target" =~ ^# ]] && continue

      if [[ "$target" == /* ]]; then
        resolved="$target"
      else
        resolved="$(cd "$(dirname "$file")" && realpath -m "$target")"
      fi

      if [[ ! -e "$resolved" ]]; then
        err "Broken link in $file -> $link (resolved: $resolved)"
      fi
    done < <(grep -oE '\[[^]]+\]\(([^)]+)\)' "$file" | sed -E 's/.*\(([^)]+)\)/\1/' || true)
  done < <(find "$all_md_dir" -maxdepth 2 -type f -name '*.md' -print0 | sort -z)
}

check_prompt_nonempty() {
  info "Checking prompt files are non-empty..."
  while IFS= read -r -d '' file; do
    if [[ ! -s "$file" ]]; then
      err "Prompt file is empty: $file"
    fi
  done < <(find "$prompt_dir" -maxdepth 1 -type f -name '*.md' -print0 | sort -z)
}

check_context_headings
check_prompt_nonempty
check_links

if [[ "$fail" -ne 0 ]]; then
  echo "FAILED: scripts/ai-context sanity checks failed." >&2
  exit 1
fi

echo "OK: scripts/ai-context sanity checks passed."
