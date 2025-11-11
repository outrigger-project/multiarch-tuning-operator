#!/bin/bash

# shellcheck disable=SC2016
# shellcheck disable=SC1004
CONTENT='# Labels from hack/patch-bundle-dockerfile.sh
LABEL com.redhat.component="Multiarch Tuning Operator"
LABEL distribution-scope="public"
LABEL name="multiarch-tuning/multiarch-tuning-operator-bundle"
LABEL release="'"${VERSION:-0.9.0}"'"
LABEL version="'"${VERSION:-0.9.0}"'"
LABEL cpe="cpe:/a:redhat:multiarch_tuning_operator:1.1::el9"
LABEL url="https://github.com/openshift/multiarch-tuning-operator"
LABEL vendor="Red Hat, Inc."
LABEL description="The Multiarch Tuning Operator enhances the user experience for administrators of Openshift \
                   clusters with multi-architecture compute nodes or Site Reliability Engineers willing to \
                   migrate from single-arch to multi-arch OpenShift"
LABEL io.k8s.description="The Multiarch Tuning Operator enhances the user experience for administrators of Openshift \
                   clusters with multi-architecture compute nodes or Site Reliability Engineers willing to \
                   migrate from single-arch to multi-arch OpenShift"
LABEL summary="The Multiarch Tuning Operator enhances the user experience for administrators of Openshift \
                   clusters with multi-architecture compute nodes or Site Reliability Engineers willing to \
                   migrate from single-arch to multi-arch OpenShift"
LABEL io.k8s.display-name="Multiarch Tuning Operator"
LABEL io.openshift.tags="openshift,operator,multiarch,scheduling"'

# Remove the content of the bundle.konflux.Dockerfile starting from the line with the comment "# Labels from hack/patch-bundle-dockerfile.sh"
if [[ "$(uname)" == "Darwin" ]]; then
    # macOS BSD sed
    sed -i '' '/# Labels from hack\/patch-bundle-dockerfile.sh/,$d' bundle.konflux.Dockerfile
else
    # Linux GNU sed
    sed -i '/# Labels from hack\/patch-bundle-dockerfile.sh/,$d' bundle.konflux.Dockerfile
fi
# Append the content to the bundle.Dockerfile and bundle.konflux.Dockerfile
cat <<EOF >>bundle.Dockerfile

$CONTENT
EOF
# DO NOT ADD an empty line for the bundle.konflux.Dockerfile.
cat <<EOF >>bundle.konflux.Dockerfile
$CONTENT
EOF
