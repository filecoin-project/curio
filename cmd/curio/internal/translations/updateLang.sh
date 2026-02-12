#!/bin/bash

# Fast translation extraction using a toy main package.
# This avoids loading the heavy dependency tree (CGO, ffi, etc.) that slows
# down gotext when scanning cmd/curio directly.
#
# ~8 seconds instead of ~3 minutes!

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CURIO_ROOT="$SCRIPT_DIR/../../../.."

# Only run if Go files have changed
if [ "$(find ../../* -newer catalog.go 2>/dev/null)" ]; then
  echo "Extracting translation strings (fast mode)..."
  
  # Create temp directory inside workspace
  WORKDIR="$SCRIPT_DIR/toyextract_tmp"
  rm -rf "$WORKDIR"
  mkdir -p "$WORKDIR"
  
  # Step 1: Extract strings from source using fast AST parser
  go run "$SCRIPT_DIR/extract.go" \
    "$CURIO_ROOT/cmd/curio" \
    "$CURIO_ROOT/cmd/curio/guidedsetup" \
    > "$WORKDIR/extracted.json" 2>&1
  
  # Step 2: Generate toy main.go (preserving existing placeholder names)
  go run "$SCRIPT_DIR/genstub.go" "$SCRIPT_DIR/locales/en/out.gotext.json" \
    < "$WORKDIR/extracted.json" \
    > "$WORKDIR/main.go"
  
  # Step 3: Initialize Go module for toy package
  cd "$WORKDIR" || exit 1
  WORKDIR_ABS="$(pwd)"
  GO111MODULE=on go mod init toyextract
  GO111MODULE=on go mod tidy
  
  # Step 4: Run gotext on the lightweight toy package only (fast!)
  # Use "." so gotext only loads this directory; GOMOD/GOWORK prevent using parent Curio.
  GOWORK=off GOMOD="$WORKDIR_ABS/go.mod" gotext -srclang=en update -out=catalog.go -lang=en,zh,ko .
  cd "$SCRIPT_DIR" || exit 1
  
  # Step 5: Copy results back to translations directory
  if [ -d "$WORKDIR/locales" ]; then
    cp -r "$WORKDIR/locales"/* "$SCRIPT_DIR/locales/"
  fi
  if [ -f "$WORKDIR/catalog.go" ]; then
    # Fix package declaration: toy package uses "main", we need "translations"
    sed 's/^package main$/package translations/' "$WORKDIR/catalog.go" > "$SCRIPT_DIR/catalog.go"
  fi
  
  # Step 6: Cleanup temp directory
  rm -rf "$WORKDIR"
  
  # Step 7: Process known translations
  go run knowns/main.go ./locales/zh ./locales/ko
  
  # Step 8: Report missing translations
  echo ""
  echo "=== Translation Status ==="
  for lang in zh ko; do
    # out.gotext.json contains strings that NEED translation
    missing=$(jq '.messages | length' "$SCRIPT_DIR/locales/$lang/out.gotext.json" 2>/dev/null || echo "?")
    if [ "$missing" = "0" ]; then
      echo "  $lang: All strings translated ✓"
    else
      echo "  $lang: $missing strings need translation"
      echo "       See locales/$lang/out.gotext.json for details"
    fi
  done
  echo ""
  if [ "$(jq '.messages | length' "$SCRIPT_DIR/locales/zh/out.gotext.json" 2>/dev/null)" != "0" ] || \
     [ "$(jq '.messages | length' "$SCRIPT_DIR/locales/ko/out.gotext.json" 2>/dev/null)" != "0" ]; then
    echo "To add translations:"
    echo "  1. Check locales/{lang}/out.gotext.json for strings needing translation"
    echo "  2. Add translations to locales/{lang}/messages.gotext.json"
    echo "  3. Run 'make gen' to compile translations into catalog.go"
    echo ""
  fi
  echo "Translation extraction complete"
fi
