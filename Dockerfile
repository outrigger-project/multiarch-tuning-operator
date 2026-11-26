ARG BUILD_IMAGE=registry.access.redhat.com/ubi9/go-toolset:1.26.10
ARG RUNTIME_IMAGE=registry.access.redhat.com/ubi9/ubi-micro:latest
ARG RUNTIME_TOOLS_IMAGE=registry.access.redhat.com/ubi9/ubi:latest
FROM ${BUILD_IMAGE} as builder
ARG TARGETOS
ARG TARGETARCH

# Switch to root to install gpgme-devel, which is required for CGO compilation of the
# containers/image library used for registry authentication and image inspection.
# This only affects the builder stage (used during compilation) and does not impact the
# security of the final runtime image, which runs as USER 65532:65532 (non-root).
USER 0
RUN if ! pkg-config --exists gpgme 2>/dev/null; then \
        if which apt-get; then apt-get update && apt-get install -y libgpgme-dev && apt-get -y clean autoclean; \
        elif which dnf; then dnf install -y gpgme-devel && dnf clean all -y; fi; \
    fi

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
COPY vendor/ vendor/

# Copy the go source
COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/
COPY pkg/ pkg/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN GOGC=75 CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go
RUN GOGC=75 CGO_ENABLED=1 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o enoexec-daemon cmd/enoexec-daemon/main.go


# The runtime image is based on UBI micro, which ships no package manager. The packages the binaries
# need at runtime are installed into a copy of the UBI micro filesystem by dnf running in a separate
# tools stage, so dnf itself never ends up in the runtime image:
# - gpgme and gnupg2: manager links libgpgme (CGO) and gpgme runs gpg to verify image signatures.
#   UBI minimal only provided them indirectly, through microdnf's dependencies.
# - ca-certificates: TLS connections to image registries.
# - openssl-libs: libcrypto and the FIPS provider, loaded at runtime by the FIPS-enabled Go toolchain.
FROM ${RUNTIME_IMAGE} as runtime-base

FROM ${RUNTIME_TOOLS_IMAGE} as runtime-rootfs
COPY --from=runtime-base / /mnt/rootfs
RUN dnf install -y --installroot /mnt/rootfs \
        --setopt install_weak_deps=False --setopt tsflags=nodocs \
        gpgme gnupg2 ca-certificates openssl-libs && \
    dnf --installroot /mnt/rootfs clean all && \
    rm -rf /mnt/rootfs/var/cache/dnf /mnt/rootfs/var/log/dnf* /mnt/rootfs/var/log/hawkey.log

# The rpm database of the populated filesystem is kept so that image scanners and SBOM tools can
# discover the installed packages.
FROM ${RUNTIME_IMAGE}
COPY --from=runtime-rootfs /mnt/rootfs/ /
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/enoexec-daemon .

USER 65532:65532
LABEL com.redhat.component="Multiarch Tuning Operator"
LABEL distribution-scope="public"
LABEL name="multiarch-tuning/multiarch-tuning-operator"
LABEL release="1.4.1"
LABEL version="1.4.1"
LABEL cpe="cpe:/a:redhat:multiarch_tuning_operator:1.4::el9"
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
LABEL io.openshift.tags="openshift,operator,multiarch,scheduling"

ENTRYPOINT ["/manager"]
