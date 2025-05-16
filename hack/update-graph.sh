#!/bin/bash

set -euo pipefail

INPUT_VERSION="$1"

# Ensure version starts with "v"
if [[ "$INPUT_VERSION" != v* ]]; then
  VERSION="v$INPUT_VERSION"
else
  VERSION="$INPUT_VERSION"
fi

ENTRY_NAME="multiarch-tuning-operator.$VERSION"

for dir in fbc-*; do
  YAML_PATH="$dir/catalog/multiarch-tuning-operator/index.yaml"

  if [[ ! -f "$YAML_PATH" ]]; then
    echo "Skipping $dir (no index.yaml found)"
    continue
  fi

  echo "Processing $YAML_PATH"

  # Extract first existing entry
  PREVIOUS_ENTRY=$(awk '
    $1 == "entries:" { in_entries=1; next }
    in_entries && $1 == "-name:" { print $2; exit }
    in_entries && $1 == "-" && $2 == "name:" { print $3; exit }
  ' "$YAML_PATH")

  if [[ -z "$PREVIOUS_ENTRY" ]]; then
    echo "Error: No previous entry found in $YAML_PATH"
    continue
  fi

  # Prepare new entry block
  NEW_ENTRY="  - name: $ENTRY_NAME\n    replaces: $PREVIOUS_ENTRY"

  # Insert the new entry after 'entries:' without adding newline at end
  awk -v new_entry="$NEW_ENTRY" '
    BEGIN { added=0 }
    {
      if (!added && $1 == "entries:") {
        print $0
        print new_entry
        added=1
      } else {
        print $0
      }
    }
    END { if (added == 0) exit 1 }
  ' "$YAML_PATH" | awk 'NR == 1 { printf "%s", $0; next } { printf "\n%s", $0 }' > "$YAML_PATH.tmp" && mv "$YAML_PATH.tmp" "$YAML_PATH"

  echo "Updated $YAML_PATH with new entry: $ENTRY_NAME replaces $PREVIOUS_ENTRY"
done

echo "Done."
