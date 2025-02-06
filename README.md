# Multiarch Tuning Operator

The Multiarch Tuning Operator enhances the operational experience within multi-architecture clusters, and
single-architecture clusters that are migrating to a multi-architecture compute configuration.

When diverse CPU architectures coexist within a cluster, the Multiarch Tuning Operator stands out as a pivotal tool to
enhance efficiency and streamline operations such as architecture-aware scheduling of workloads.

The operator is available as part of the redhat-operator catalog in Openshift clusters.

There is no official, general available, upstream release of the operator yet.

## Operands

### Architecture-aware Pod Placement

The pod placement operand consists of
the `pod placement controller` and the `pod placement webhook`. It is managed through
a singleton custom resource: `clusterpodplacementconfigs.multiarch.openshift.io`.

When a pod is created, the operand performs the following actions:

1. Add the `multiarch.openshift.io/scheduling-gate` scheduling gate that prevents the scheduling of the pod.
2. Compute a scheduling predicate that includes the supported architecture values for the `kubernetes.io/arch`.
3. Integrate the scheduling predicate as a `nodeAffinity` requirement in the pod specification.
4. Remove the scheduling gate from the pod.

When the operand removes the scheduling gate, the pod enters the scheduling cycle. 
The workload is then scheduled on nodes based on the supported architectures.

This operand is based on the [KEP-3521](https://github.com/kubernetes/enhancements/blob/afad6f270c7ac2ae853f4d1b72c379a6c3c7c042/keps/sig-scheduling/3521-pod-scheduling-readiness/README.md) and
[KEP-3838](https://github.com/kubernetes/enhancements/blob/afad6f270c7ac2ae853f4d1b72c379a6c3c7c042/keps/sig-scheduling/3838-pod-mutable-scheduling-directives/README.md), as
described in the [Openshift EP](https://github.com/openshift/enhancements/blob/6cebc13f0672c601ebfae669ea4fc8ca632721b5/enhancements/multi-arch/multiarch-manager-operator.md) introducing it.

## Getting Started

The aim of this operator will be to run on any Kubernetes cluster, although the main focus of development and testing
will be carried out on Openshift clusters.

## Enable metrics

The operator can expose metrics for the prometheus operator. To enable the deployment of the metrics objects (`monitoring.coreos.com/(servicemonitors|prometheusrules)`), you can label the namespace where the operator runs with `k8s.io/cluster-monitoring: "true"`. 

Use `openshift.io/cluster-monitoring: "true"` for Openshift clusters.

### Development

### Prerequisites

The development tooling of this repository depends on the following tools/technologies:

- Golang
- Docker (Community Edition)
- Development headers and libraries for gpgme (`gpgme-devel` in Fedora/CS9/RHEL, `libgpgme-dev` in Debian)
- `operator-sdk`
- `make`
- `qemu-user-static` (to support building multi-arch container images via `docker buildx`)

### Build the operator

```shell
# Multi-arch image build
make docker-buildx IMG=<some-registry>/multiarch-tuning-operator:tag

# Single arch image build
make docker-build IMG=<some-registry>/multiarch-tuning-operator:tag

# Local build
make build
```

If you aim to use the multi-arch build and would avoid the deletion of the buildx instance, you can
create an empty `.persistent-buildx` file in the root of the repository.

```shell
touch .persistent-buildx
make docker-buildx IMG=<some-registry>/multiarch-tuning-operator:tag
```

### Deploy the operator

```shell
# Deploy the operator on the cluster
make deploy IMG=<some-registry>/multiarch-tuning-operator:tag
```

### Deploy the Pod Placement Operand

To enable architecture-aware workload scheduling, you must create the `ClusterPodPlacementConfig` object.

When you create the `ClusterPodPlacementConfig` object, this Operator deploys the pod placement operand.

The `ClusterPodPlacementConfig` is a singleton object: the API allows only one object with name `cluster`.

The following is an example of a ClusterPodPlacementConfig CR that sets the log verbosity level to `Normal` and 
will watch and setup CPU architecture node affinities on all the pods, except the ones in namespaces labeled with 
`multiarch.openshift.io/exclude-pod-placement`.

**Note**: the namespaces `openshift-*`, `kube-*1`, and `hypershift-*` are excluded by default and cannot be included back for
safety reasons.

```shell
# Deploy the pod placement operand on the cluster
kubectl create -f - <<EOF
apiVersion: multiarch.openshift.io/v1beta1
kind: ClusterPodPlacementConfig
metadata:
  name: cluster
spec:
  logVerbosityLevel: Normal
  namespaceSelector:
    matchExpressions:
      - key: multiarch.openshift.io/exclude-pod-placement
        operator: DoesNotExist
EOF
```

### Undeploy the ClusterPodPlacementConfig operand

```shell
kubectl delete clusterpodplacementconfigs/cluster
```

Ordered uninstallation of the operand will be implemented in the future, and will remove the pod placement controller
only after all the scheduling gated pods have been ungated.

To overcome this limitation currently, you can execute the following to ensure the deletion of the scheduling gate 
from all the pods:
```shell
kubectl get pods -A -l multiarch.openshift.io/scheduling-gate=gated -o json  | jq 'del(.items[].spec.schedulingGates[] | select(.name=="multiarch.openshift.io/scheduling-gate"))' | kubectl apply -f -
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller

UnDeploy the controller from the cluster:

```sh
make undeploy
```

### Modifying the API definitions

If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

### Running tests

The following units are available as Makefile targets:

```shell
# Lint
make lint
# gosec (SAST)
make gosec
# vet
make vet
# goimports
make goimports
# gofmt
make fmt
# Run unit tests
make test
# Run e2e tests (after the operator is deployed, e.g., via `make deploy`)
KUBECONFIG=/path/to/cluster/kubeconfig NAMESPACE=openshift-multiarch-tuning-operator make e2e 
```

All the checks run on a containerized environment by default. 
You can run them locally by setting the `NO_DOCKER` variable to `1`:

```shell
NO_DOCKER=1 make test
```

or adding the `NO_DOCKER=1` row in the `.env` file.

See the [dotenv.example](./dotenv.example) file for other available settings.

### How it works

See [Openshift Enhancement Proposal](https://github.com/openshift/enhancements/blob/6cebc13f0672c601ebfae669ea4fc8ca632721b5/enhancements/multi-arch/multiarch-manager-operator.md).


## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

## License

Copyright 2023 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

