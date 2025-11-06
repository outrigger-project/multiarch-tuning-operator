#!/bin/sh

# Usage function
usage() {
  echo "Usage: $0 [-v <version-with-dash>] [-s <snapshot-name>]"
  exit 1
}

# Default version if none provided
DEFAULT_VERSION="v1-x"

# Parse options
while getopts "v:s:" opt; do
  case "$opt" in
    v) VERSION="$OPTARG" ;;
    s) SNAPSHOT="$OPTARG" ;;
    *) usage ;;
  esac
done

# Shift away parsed options, now $@ holds remaining unparsed arguments
shift $((OPTIND - 1))

# If there are any remaining arguments, show usage (e.g., user ran: ./script.sh 1.x)
if [ "$#" -gt 0 ]; then
  echo "Error: Unrecognized argument(s): $*"
  usage
fi

# Set VERSION to default if not provided
if [ -z "$VERSION" ]; then
  echo "No version supplied. Using default: $DEFAULT_VERSION"
  VERSION="$DEFAULT_VERSION"
fi

# Validate VERSION format
if echo "$VERSION" | grep -q "\."; then
  echo "Error: Version format invalid. Use dashes instead of dots (e.g., v1-0, not v1.0)"
  exit 1
fi

if ! echo "$VERSION" | grep -q "-"; then
  echo "Error: Version must contain at least one dash (e.g., v1-0)"
  exit 1
fi

# If SNAPSHOT is not provided, list and let user select
if [ -z "$SNAPSHOT" ]; then
  echo "Fetching snapshot list for version $VERSION..."
  SNAPSHOTS=$(oc get snapshots --sort-by .metadata.creationTimestamp \
    -l pac.test.appstudio.openshift.io/event-type=push,appstudio.openshift.io/application=multiarch-tuning-operator-${VERSION} \
    --no-headers -o custom-columns=":metadata.name")

if [ -z "$SNAPSHOTS" ]; then
  echo "No snapshots found for version: $VERSION"
  echo "Please verify the version name and ensure you are logged in to Konflux."
  exit 1
fi


  echo "Available snapshots:"
  echo "$SNAPSHOTS" | nl

  echo "Enter the number of the snapshot you want to use:"
  read -r SELECTION

  SNAPSHOT=$(echo "$SNAPSHOTS" | sed -n "${SELECTION}p")
  if [ -z "$SNAPSHOT" ]; then
    echo "Invalid selection"
    exit 1
  fi
fi

echo "Using snapshot: $SNAPSHOT"
echo "Fetching snapshot YAML for: $SNAPSHOT"
SNAPSHOT_YAML=$(oc get snapshot "$SNAPSHOT" -o yaml)

# Extract container images using yq
IMAGES=$(echo "$SNAPSHOT_YAML" | yq '.spec.components[].containerImage')

echo "Container images in snapshot are:"
echo "$IMAGES"

# Extract the bundle image (contains "bundle" in the name)
BUNDLE_IMAGE=$(echo "$IMAGES" | grep "bundle")

if [ -z "$BUNDLE_IMAGE" ]; then
  echo "Error: No bundle image found in snapshot components."
  exit 1
fi

echo "Using bundle image: $BUNDLE_IMAGE"

# Pull the image using podman
echo "Pulling bundle image..."
podman pull "$BUNDLE_IMAGE"

# Save and extract the image
BUNDLE_TAR="/tmp/operator_bundle.tar"
BUNDLE_DIR="/tmp/operator_bundle"

echo "Saving image to $BUNDLE_TAR..."
podman save -o "$BUNDLE_TAR" "$BUNDLE_IMAGE"

echo "Extracting image to $BUNDLE_DIR..."
mkdir -p "$BUNDLE_DIR"
tar -xf "$BUNDLE_TAR" -C "$BUNDLE_DIR"

#Extract internal layer tarballs (usually contain the operator files)
echo "Extracting inner layer tarballs..."

for layer_tar in "$BUNDLE_DIR"/*.tar; do
  echo "Unpacking layer: $layer_tar"
  tar -xf "$layer_tar" -C "$BUNDLE_DIR"
done


# Find the CSV file
CSV_FILE=$(find "$BUNDLE_DIR" -name "*.clusterserviceversion.yaml" | head -n 1)

if [ -z "$CSV_FILE" ]; then
  echo "Error: .clusterserviceversion.yaml not found in the extracted bundle."
  exit 1
fi

echo "Found CSV file: $CSV_FILE"

# Extract the internal image from annotations
INTERNAL_IMAGE=$(yq '.spec.install.spec.deployments[0].spec.template.metadata.annotations."multiarch.openshift.io/image"' "$CSV_FILE")

echo "Internal image referenced in the CSV:"
echo "$INTERNAL_IMAGE"


# Extract SHA from INTERNAL_IMAGE
INTERNAL_SHA=$(echo "$INTERNAL_IMAGE" | sed -n 's/.*@sha256:\([a-f0-9]\+\)$/\1/p')

if [ -z "$INTERNAL_SHA" ]; then
  echo "Error: Could not extract SHA from internal image."
  exit 1
fi

echo "Internal image SHA: $INTERNAL_SHA"

# Extract SHAs from all snapshot images into an array
SNAPSHOT_SHAS=$(echo "$IMAGES" | sed -n 's/.*@sha256:\([a-f0-9]\+\)$/\1/p')

# Compare INTERNAL_SHA to each SHA from the snapshot images
MATCH_FOUND=0
for sha in $SNAPSHOT_SHAS; do
  if [ "$sha" = "$INTERNAL_SHA" ]; then
    MATCH_FOUND=1
    break
  fi
done

if [ $MATCH_FOUND -eq 1 ]; then
  # Green text
  echo -e "\033[32mMatch found: The internal image SHA is present in snapshot images.\033[0m"
else
  # Red text
  echo -e "\033[31mNo match: The internal image SHA is NOT present in snapshot images.\033[0m"
fi

rm -rf $BUNDLE_TAR $BUNDLE_DIR