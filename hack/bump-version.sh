#!/bin/bash

yq -i ".spec.version=\"${VERSION:-1.0.0}\"" config/manifests/bases/multiarch-tuning-operator.clusterserviceversion.yaml
yq -i ".metadata.name=\"multiarch-tuning-operator.v${VERSION:-1.0.0}\"" config/manifests/bases/multiarch-tuning-operator.clusterserviceversion.yaml
yq -i ".spec.startingCSV=\"multiarch-tuning-operator.v${VERSION:-1.0.0}\"" deploy/base/operators.coreos.com/subscriptions/openshift-multiarch-tuning-operator/subscription.yaml
sed -i "s/^LABEL release=.*/LABEL release=\"${VERSION:-1.0.0}\"/" Dockerfile
sed -i "s/^LABEL version=.*/LABEL version=\"${VERSION:-1.0.0}\"/" Dockerfile
sed -i "s/^LABEL release=.*/LABEL release=\"${VERSION:-1.0.0}\"/" konflux.Dockerfile
sed -i "s/^LABEL version=.*/LABEL version=\"${VERSION:-1.0.0}\"/" konflux.Dockerfile
sed -i "s/^VERSION ?= .*/VERSION ?= ${VERSION:-1.0.0}/" Makefile
make bundle