# Install Multiarch Tuning Operator in a kind Cluster with CRI-O

There are several OpenShift-specific dependencies required to run the Multiarch Tuning Operator on non-OpenShift clusters. We already have an enhancement and an epic [MULTIARCH-5324](https://issues.redhat.com/browse/MULTIARCH-5324) planned to remove these dependencies.
In the meantime, if you want to use the operator before that enhancement is complete, this document provides instructions for installing the Multiarch Tuning Operator on a `kind` cluster that uses `CRI-O` as the container runtime.
It includes steps for creating the cluster, installing dependencies, and deploying the operator.

## Prerequisites

- Golang
- Docker (Community Edition)
- kind
- Helm
- kubectl

## Create kind Cluster with CRI-O

The following is an example setup:

### Ensure Kind is installed

```bash
go install sigs.k8s.io/kind@v0.29.0
kind version
```
more information see [installation-and-usage](https://kind.sigs.k8s.io/#installation-and-usage)

### Create a CRI-O–based node image
You can follow the official guide [crio-in-kind](https://github.com/cri-o/cri-o/blob/main/tutorials/crio-in-kind.md) to build a node image that uses CRI-O as the container runtime.

### Create a Kind configuration file using the CRI-O–based node image
To enable image policy and registry access, the operator depends on standard container runtime configuration files, such as:
`/etc/containers/policy.json`
`/etc/containers/registries.conf`
As these files are not always present by default, we recommend mounting the corresponding host paths into the container to provide the necessary configuration context.
Use the node image created in the previous step to define the cluster configuration. Save the following content into a file named `kind-crio-config.yaml`:
```yaml
cat <<EOF > kind-crio-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    image: kindnode/crio:v1.30
    extraMounts:
    - hostPath: /etc/containers/registries.conf
      containerPath: /etc/containers/registries.conf
    - hostPath: /etc/containers/policy.json
      containerPath: /etc/containers/policy.json
    extraPortMappings:
      - containerPort: 6443
        hostPort: 6443
        listenAddress: "127.0.0.1"
    kubeadmConfigPatches:
      - |
        kind: InitConfiguration
        nodeRegistration:
          criSocket: unix:///var/run/crio/crio.sock
      - |
        kind: JoinConfiguration
        nodeRegistration:
          criSocket: unix:///var/run/crio/crio.sock
  - role: worker
    image: kindnode/crio:v1.30
    extraMounts:
    - hostPath: /etc/containers/registries.conf
      containerPath: /etc/containers/registries.conf
    - hostPath: /etc/containers/policy.json
      containerPath: /etc/containers/policy.json
    extraPortMappings:
      - containerPort: 443
        hostPort: 443
        listenAddress: "127.0.0.1"
    kubeadmConfigPatches:
      - |
        kind: JoinConfiguration
        nodeRegistration:
          criSocket: unix:///var/run/crio/crio.sock
EOF
```

### Run below command to create a basic crio kind cluster:

```bash
kind create cluster --name cri-o --config kind-crio-config.yaml
```

## Create certificate for operator traffic encryption
To ensure the operator traffic is secure, we use certificates to encrypt the communication.
In OpenShift (OCP) clusters, certificate-related resources are automatically managed by the ocp operator. However, in non-OCP environments, you must manage these certificates manually. Also you can use a `cert-manager` operator to assist with certificate provisioning and management.

### Install cert-manager

```bash
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set installCRDs=true
```
### Create a ClusterIssuer using the cert-manager's default CA secret

```bash
kubectl create -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: multiarch-tuning-operator-issuer
spec:
  ca:
    secretName: cert-manager-webhook-ca
EOF
```

### Create Namespace and Certificates for the Operator
To run the Multiarch Tuning Operator, you need to provision TLS certificates for the following services and webhooks:
`multiarch-tuning-operator-controller-manager-service-cert`
`pod-placement-controller`
`pod-placement-web-hook`
`ValidatingWebhookConfiguration`
`MutatingWebhookConfiguration`

These certificates should be created as Kubernetes Secrets in the operator's namespace.
To automate their creation and renewal, define corresponding `Certificate` resources managed by `cert-manager`.

First, create the namespace:
```bash
kubectl create ns openshift-multiarch-tuning-operator
```

Then, apply the Certificate manifests to issue and manage certificates for each service:
```bash
kubectl create -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: multiarch-tuning-operator-controller-manager-service-cert
  namespace: openshift-multiarch-tuning-operator
spec:
  secretName: multiarch-tuning-operator-controller-manager-service-cert
  issuerRef:
    name: multiarch-tuning-operator-issuer
    kind: ClusterIssuer
  commonName: openshift-multiarch-tuning-operator.svc
  dnsNames:
    - multiarch-tuning-operator-controller-manager-service.openshift-multiarch-tuning-operator.svc
    - multiarch-tuning-operator-controller-manager-service.openshift-multiarch-tuning-operator.svc.cluster.local
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: multiarch-tuning-operator-pod-placement-controller
  namespace: openshift-multiarch-tuning-operator
spec:
  secretName: pod-placement-controller
  issuerRef:
    name: multiarch-tuning-operator-issuer
    kind: ClusterIssuer
  commonName: openshift-multiarch-tuning-operator.svc
  dnsNames:
    - pod-placement-controller.openshift-multiarch-tuning-operator.svc
    - pod-placement-controller.openshift-multiarch-tuning-operator.svc.cluster.local
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: multiarch-tuning-operator-pod-placement-web-hook
  namespace: openshift-multiarch-tuning-operator
spec:
  secretName: pod-placement-web-hook
  issuerRef:
    name: multiarch-tuning-operator-issuer
    kind: ClusterIssuer
  commonName: openshift-multiarch-tuning-operator.svc
  dnsNames:
    - pod-placement-web-hook.openshift-multiarch-tuning-operator.svc
    - pod-placement-web-hook.openshift-multiarch-tuning-operator.svc.cluster.local
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: multiarch-tuning-operator-validating-webhook
  namespace: openshift-multiarch-tuning-operator
spec:
  secretName: validating-webhook-tls
  dnsNames:
    - validating-webhook.openshift-multiarch-tuning-operator.svc
  issuerRef:
    name: multiarch-tuning-operator-issuer
    kind: ClusterIssuer
  commonName: validating-webhook.openshift-multiarch-tuning-operator.svc
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: multiarch-tuning-operator-mutating-webhook
  namespace: openshift-multiarch-tuning-operator
spec:
  secretName: mutating-webhook-tls
  dnsNames:
    - mutating-webhook.openshift-multiarch-tuning-operator.svc
  issuerRef:
    name: multiarch-tuning-operator-issuer
    kind: ClusterIssuer
  commonName: mutating-webhook.openshift-multiarch-tuning-operator.svc
EOF
```

## Add CA Bundle ConfigMap (if needed)
In OpenShift, resources labeled with `config.openshift.io/inject-trusted-cabundle: "true"` are automatically injected with the cluster’s trusted CA bundle.
The Multiarch Tuning Operator expects a ConfigMap named `multiarch-tuning-operator-trusted-ca` to contain this bundle. This ConfigMap is typically labeled to allow automatic CA injection in OpenShift clusters.
However, in non-OpenShift Kubernetes environments, this injection does not occur automatically. To ensure proper functionality, you must manually create the trusted CA ConfigMap as follows:

### Extract the system CA bundle from the node (example using Kind):
```bash
docker cp <node_name>:/etc/ssl/certs/ca-certificates.crt ./ca-certificates.crt
```

### Create the ConfigMap with the CA bundle:
```bash
kubectl create configmap multiarch-tuning-operator-trusted-ca \
  --from-file=ca-bundle.crt=./ca-certificates.crt \
  -n openshift-multiarch-tuning-operator
```

## Add Pull Secret
By default, the operator is hardcoded to watch the `pull-secret` Secret in the `openshift-config` namespace.
If you're running on a non-OpenShift cluster or if this namespace does not exist, you need to manually create the `openshift-config` namespace and add the `pull-secret` Secret to it.

To support custom configurations, the operator also provides the following parameters:
- `global-pull-secret-namespace`: to specify the namespace
- `global-pull-secret-name`: to specify the secret name

You can pass these parameters as environment variables by modifying the (manager.yaml)[https://github.com/outrigger-project/multiarch-tuning-operator/blob/799f91a33ceb2f1b503f97016ce9e44b30322909/config/manager/manager.yaml#L68] file, and then build your own Multiarch Tuning Operator (MTO) image to include the updated configuration.
For example:
 ```bash
 - "--global-pull-secret-namespace=yoursecretnamespace"
 - "--global-pull-secret-name=yoursecretname"
```

If you already have a Docker configuration file for accessing the secured registry, you can create the pull secret using the following command (assuming the config file is located at .docker/config.json):
```bash
kubectl create namespace openshift-config

kubectl -n openshift-config create secret generic pull-secret \
  --from-file=.dockerconfigjson=path/to/.docker/config.json \
  --type=kubernetes.io/dockerconfigjson
```
For more details, see the official documentation:
[allowing-pods-to-reference-images-from-other-secured-registries](https://docs.redhat.com/en/documentation/openshift_container_platform/3.1/html/developer_guide/dev-guide-image-pull-secrets#allowing-pods-to-reference-images-from-other-secured-registries)

## Deploy Multiarch Tuning Operator and Patch CA bundle for The Webhook Configuration

Clone the repository and run the following command to install the operator and its operand:
```bash
make deploy IMG=registry.ci.openshift.org/origin/multiarch-tuning-operator:main

kubectl annotate ValidatingWebhookConfiguration multiarch-tuning-operator-validating-webhook-configuration \
cert-manager.io/inject-ca-from=openshift-multiarch-tuning-operator/multiarch-tuning-operator-validating-webhook

kubectl create -f - <<EOF
apiVersion: multiarch.openshift.io/v1beta1
kind: ClusterPodPlacementConfig
metadata:
  name: cluster
spec:
  namespaceSelector:
    matchExpressions:
      - key: multiarch.openshift.io/exclude-pod-placement
        operator: DoesNotExist
  plugins:
    nodeAffinityScoring:
      enabled: true
      platforms:
        - architecture: arm64
          weight: 50
EOF

kubectl annotate MutatingWebhookConfiguration pod-placement-mutating-webhook-configuration \
cert-manager.io/inject-ca-from=openshift-multiarch-tuning-operator/multiarch-tuning-operator-mutating-webhook
```

Then, you can use the Multiarch Tuning Operator to enable `architecture-aware` workload scheduling, improving cluster efficiency and operational consistency.