#!/bin/bash
# Checks that a runtime image has what the shipped binaries need at runtime, and no package manager.
#
# Usage: hack/verify-runtime-image.sh <image>
# Environment:
#   ENGINE               container engine to use (default: podman)
#   RUNTIME_TOOLS_IMAGE  image providing rpm to list the image packages (podman only,
#                        default: registry.access.redhat.com/ubi9/ubi:latest)

set -euo pipefail

IMAGE=${1:?"usage: $0 <image>"}
ENGINE=${ENGINE:-podman}
RUNTIME_TOOLS_IMAGE=${RUNTIME_TOOLS_IMAGE:-registry.access.redhat.com/ubi9/ubi:latest}

failed=0
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*"; failed=1; }

echo "Verifying runtime image ${IMAGE}"

# Image configuration
user=$("${ENGINE}" image inspect --format '{{.Config.User}}' "${IMAGE}")
if [ "${user}" = "65532:65532" ]; then pass "runs as user ${user}"; else fail "runs as user '${user}', expected 65532:65532"; fi
entrypoint=$("${ENGINE}" image inspect --format '{{json .Config.Entrypoint}}' "${IMAGE}")
if [ "${entrypoint}" = '["/manager"]' ]; then pass "entrypoint ${entrypoint}"; else fail "entrypoint ${entrypoint}, expected [\"/manager\"]"; fi

# Checks run inside the image, as the image user.
# shellcheck disable=SC2016 # the script is expanded inside the container
if ! "${ENGINE}" run --rm --entrypoint /bin/sh "${IMAGE}" -c '
failed=0
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*"; failed=1; }

for tool in microdnf dnf yum rpm; do
    if command -v "${tool}" >/dev/null 2>&1; then fail "package manager tool present: ${tool}"; else pass "no ${tool}"; fi
done

# -h prints the flags and exits. If the dynamic loader cannot find a shared library the binary links
# (libgpgme, libassuan, libgpg-error, glibc), the binary does not start: the loader prints an error
# and exits with 127.
for binary in /manager /enoexec-daemon; do
    rc=0
    out=$("${binary}" -h 2>&1) || rc=$?
    case "${out}" in
        *"error while loading shared libraries"*) fail "${binary} cannot load its shared libraries: ${out}" ;;
        *) if [ "${rc}" -eq 127 ]; then fail "${binary} does not start (exit 127): ${out}"; else pass "${binary} starts"; fi ;;
    esac
done

# gpgme runs gpg to verify image signatures.
if gpg --version >/dev/null 2>&1; then pass "gpg available"; else fail "gpg missing"; fi
if [ -s /etc/pki/tls/certs/ca-bundle.crt ]; then pass "CA bundle present"; else fail "CA bundle missing"; fi
# The FIPS-enabled Go toolchain loads libcrypto and the OpenSSL FIPS provider at runtime.
if [ -e /usr/lib64/libcrypto.so.3 ]; then pass "libcrypto present"; else fail "libcrypto missing"; fi
if [ -e /usr/lib64/ossl-modules/fips.so ]; then pass "OpenSSL FIPS provider present"; else fail "OpenSSL FIPS provider missing"; fi
if [ -e /var/lib/rpm/rpmdb.sqlite ] || [ -e /usr/lib/sysimage/rpm/rpmdb.sqlite ]; then
    pass "rpm database present"
else
    fail "rpm database missing: scanners cannot list the image packages"
fi
exit "${failed}"
'; then
    failed=1
fi

# Package inventory, read from the image rpm database as a scanner would (podman only).
if [ "${ENGINE}" = "podman" ]; then
    if packages=$(podman run --rm --mount "type=image,source=${IMAGE},destination=/target" \
            "${RUNTIME_TOOLS_IMAGE}" rpm --root /target -qa --qf '%{NAME}\n' | sort); then
        pass "rpm inventory readable: $(echo "${packages}" | wc -l) packages"
        echo "${packages}" | tr '\n' ' '
        echo
    else
        fail "cannot read the rpm inventory"
    fi
fi

echo "Image size: $("${ENGINE}" image inspect --format '{{.Size}}' "${IMAGE}" | numfmt --to=si)B"

if [ "${failed}" -ne 0 ]; then
    echo "Runtime image verification FAILED"
    exit 1
fi
echo "Runtime image verification passed"
