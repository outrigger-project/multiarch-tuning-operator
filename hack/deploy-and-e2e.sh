#!/bin/bash
set -euxo pipefail
trap debug ERR

function debug() {
  echo "An error occurred in the script at line: ${BASH_LINENO[0]}."
  set +e
  for r in pods deployments events subscriptions clusterserviceversions clusterpodplacementconfigs; do
    oc get ${r} -n "${NAMESPACE}" -o yaml > "${ARTIFACT_DIR}/${r}.yaml"
    oc describe ${r} -n "${NAMESPACE}" | tee "${ARTIFACT_DIR}/${r}.txt"
    oc get ${r} -n "${NAMESPACE}" -o wide
  done
  echo "Exiting script."
  exit 1
}

mkdir -p /tmp/bin
export PATH=/tmp/bin:${PATH}
if ! which kubectl >/dev/null; then
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
  export JUNIT_SUFFIX="-olm"
  operator-sdk run bundle "${OO_BUNDLE}" -n "${NAMESPACE}" --security-context-config restricted --timeout=10m
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

if [ "${CLEANUP:-false}" == "false" ]; then
  exit 0
fi

set +e
[[ "$USE_OLM" == "true" ]] && operator-sdk cleanup multiarch-tuning-operator -n ${NAMESPACE}
[[ "$USE_OLM" == "false" ]] && make undeploy
oc delete --ignore-not-found --force namespace ${NAMESPACE}
exit 0
