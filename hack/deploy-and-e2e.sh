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

if [ "${USE_OLM:-}" == "true" ]; then
  export HOME=/tmp/home
  export XDG_RUNTIME_DIR=/tmp/home/containers
  OLD_KUBECONFIG=${KUBECONFIG}

  mkdir -p $XDG_RUNTIME_DIR
  unset KUBECONFIG
  # The following is required for prow, we allow failures as in general we don't expect
  # this to be required in non-prow envs, for example dev environments.
  oc registry login || echo "[WARN] Unable to login the registry, this could be expected in non-Prow envs"

  export KUBECONFIG="${OLD_KUBECONFIG}"
  operator-sdk run bundle "${OO_BUNDLE}" -n "${NAMESPACE}"
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
