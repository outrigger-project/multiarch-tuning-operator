#!/bin/bash
set -euxo pipefail

if ! which kubectl >/dev/null; then
  mkdir -p /tmp/bin
  export PATH=/tmp/bin:${PATH}
  ln -s "$(which oc)" "/tmp/bin/kubectl"
fi

export NO_DOCKER=1
export NAMESPACE=openshift-multiarch-tuning-operator
oc create namespace ${NAMESPACE}
oc annotate namespace ${NAMESPACE} \
  workload.openshift.io/allowed="management"

if [ "${USE_OLM:-}" == "true" ]; then
  # Get the manifest from the manifest-list
  # Prow produces a manifest-list image even for the bundle and that bundle image is not deployed on the multi-arch clusters
  # Therefore, it can be a single-arch one and operator-sdk isn't able to extract the bundle as the bundle image is set in a
  # pod's container image field and cri-o will fail to pull the image when the architecture of the node is different from the
  # bundle image's architecture.
  # However, the bundle image is FROM scratch and doesn't have any architecture-specific binaries. It doesn't need to be
  # a manifest-list image. Therefore, we can extract the first single-arch manifest from the manifest-list image and use it
  # as the bundle image in a multi-arch cluster, allowing the extraction pod to be scheduled on arm64 as well.
  # The following is a workaround for this issue until https://issues.redhat.com/browse/DPTP-4143 is resolved.
  if oc image info --show-multiarch "${OO_BUNDLE}" | grep -q "Manifest List:"; then
    MANIFEST_DIGEST=$(oc image info --show-multiarch "${OO_BUNDLE}" | grep "Digest: " | awk '{print $2}' | head -n1)
    OO_BUNDLE=${OO_BUNDLE%%:*}
    OO_BUNDLE=${OO_BUNDLE%%@*}@${MANIFEST_DIGEST}
  fi
  export HOME=/tmp/home
  export XDG_RUNTIME_DIR=/tmp/home/containers
  OLD_KUBECONFIG=${KUBECONFIG}

  mkdir -p $XDG_RUNTIME_DIR
  unset KUBECONFIG
  # The following is required for prow, we allow failures as in general we don't expect
  # this to be required in non-prow envs, for example dev environments.
  oc registry login || echo "[WARN] Unable to login the registry, this could be expected in non-Prow envs"

  export KUBECONFIG="${OLD_KUBECONFIG}"
  operator-sdk run bundle "${OO_BUNDLE}" -n "${NAMESPACE}" --security-context-config restricted
else
  make deploy IMG="${OPERATOR_IMAGE}"
fi

oc wait deployments -n ${NAMESPACE} \
  -l app.kubernetes.io/part-of=multiarch-tuning-operator \
  --for=condition=Available=True
oc wait pods -n ${NAMESPACE} \
  -l control-plane=controller-manager \
  --for=condition=Ready=True

make e2e
