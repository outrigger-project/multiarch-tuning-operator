#!/bin/bash

set -e

# Accept version as first argument, fall back to VERSION env var, or show usage
if [ -n "$1" ]; then
    VERSION="$1"
elif [ -z "$VERSION" ]; then
    echo "Usage: $0 <version>"
    echo "  or set VERSION environment variable"
    echo "Example: $0 1.2.1"
    exit 1
fi

echo "Bumping version to: $VERSION"

yq -i ".spec.version=\"${VERSION}\"" config/manifests/bases/multiarch-tuning-operator.clusterserviceversion.yaml
yq -i ".metadata.name=\"multiarch-tuning-operator.v${VERSION}\"" config/manifests/bases/multiarch-tuning-operator.clusterserviceversion.yaml
yq -i ".spec.startingCSV=\"multiarch-tuning-operator.v${VERSION}\"" deploy/base/operators.coreos.com/subscriptions/openshift-multiarch-tuning-operator/subscription.yaml
yq eval-all -i "(select(.schema==\"olm.channel\").entries[0].name)=\"multiarch-tuning-operator.v${VERSION}\"" index.base.yaml


if [[ "$(uname)" == "Darwin" ]]; then
    # macOS BSD sed
    sed -i '' "s/^LABEL release=.*/LABEL release=\"${VERSION}\"/" Dockerfile
    sed -i '' "s/^LABEL version=.*/LABEL version=\"${VERSION}\"/" Dockerfile
    sed -i '' "s/^LABEL release=.*/LABEL release=\"${VERSION}\"/" konflux.Dockerfile
    sed -i '' "s/^LABEL version=.*/LABEL version=\"${VERSION}\"/" konflux.Dockerfile
    sed -i '' "s/^VERSION ?= .*/VERSION ?= ${VERSION}/" Makefile
else
    # Linux GNU sed
    sed -i "s/^LABEL release=.*/LABEL release=\"${VERSION}\"/" Dockerfile
    sed -i "s/^LABEL version=.*/LABEL version=\"${VERSION}\"/" Dockerfile
    sed -i "s/^LABEL release=.*/LABEL release=\"${VERSION}\"/" konflux.Dockerfile
    sed -i "s/^LABEL version=.*/LABEL version=\"${VERSION}\"/" konflux.Dockerfile
    sed -i "s/^VERSION ?= .*/VERSION ?= ${VERSION}/" Makefile
fi
echo "make bundle"
make bundle