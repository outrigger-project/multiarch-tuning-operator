#!/bin/bash

set -euo pipefail

INPUT="$1"
BUNDLE_IMAGE=""

# Function to extract bundle image from snapshot
get_bundle_image_from_snapshot() {
    local snapshot_name="$1"
    echo "Getting bundle image from snapshot: $snapshot_name" >&2
    oc describe snapshot "$snapshot_name" | \
        awk '/Container Image:/ && /bundle/ {print $NF; exit}'
}


# Detect if input is a snapshot or a direct image URL
if [[ "$INPUT" == quay.io/* || "$INPUT" == *"@"* ]]; then
    echo "Using provided bundle image URL directly."
    BUNDLE_IMAGE="$INPUT"
else
    echo "Input appears to be a snapshot. Extracting bundle image..."
    BUNDLE_IMAGE=$(get_bundle_image_from_snapshot "$INPUT")
    if [[ -z "$BUNDLE_IMAGE" ]]; then
        echo "Error: Could not find bundle image in snapshot $INPUT"
        exit 1
    fi
    echo "Found bundle image: $BUNDLE_IMAGE"
fi


# Yellow color escape code
YELLOW='\033[1;33m'
# Reset color escape code
NC='\033[0m'

echo -e "${YELLOW}Warning: verify opm version is updated${NC}"

# Loop over directories that match the pattern fbc-v*-*
for dir in fbc-v*-*; do
    if [[ -d "$dir" ]]; then
        echo "Processing directory: $dir"

        # Extract version from directory name (e.g., 4-16 from fbc-v4-16)
        version=$(echo "$dir" | sed -E 's/^fbc-v([0-9]+-[0-9]+).*/\1/')

        pushd "$dir/catalog/multiarch-tuning-operator" > /dev/null

        if [[ ! -f index.yaml ]]; then
            echo "Warning: index.yaml not found in $dir/catalog/multiarch-tuning-operator"
            popd > /dev/null
            continue
        fi

        if [[ "$version" < "4-17" ]]; then
            echo "" >> index.yaml
            opm render --output=yaml "$BUNDLE_IMAGE" >> index.yaml
        else
          echo "Using migrate-level option for version $version"
          echo "" >> index.yaml
          opm render --output=yaml --migrate-level bundle-object-to-csv-metadata "$BUNDLE_IMAGE" >> index.yaml
        fi
        # Fix top-level image field
        sed -i -E 's|(image: )quay.io/redhat-user-workloads[^@]*multiarch-tuning-operator-bundle[^@]*@sha256:([a-f0-9]{64})|\1registry.redhat.io/multiarch-tuning/multiarch-tuning-operator-bundle@sha256:\2|' index.yaml

        # Fix relatedImages entries
        sed -i -E 's|(- image: )quay.io/redhat-user-workloads[^@]*multiarch-tuning-operator-bundle[^@]*@sha256:([a-f0-9]{64})|\1registry.redhat.io/multiarch-tuning/multiarch-tuning-operator-bundle@sha256:\2|' index.yaml


        echo "Appended bundle data to $dir/catalog/multiarch-tuning-operator/index.yaml"
        popd > /dev/null
    fi
done

echo "Done."
