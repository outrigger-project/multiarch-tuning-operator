#!/bin/bash
# File update functions for multiarch-tuning-operator upgrades
#
# This library provides functions to update specific files during version upgrades.
# All regex patterns and file locations are operator-specific.

set -euo pipefail

# Portable sed in-place function that works on both GNU sed (Linux) and BSD sed (macOS)
# Usage: sed_inplace 's/pattern/replacement/' file
sed_inplace() {
    local pattern="$1"
    local file="$2"

    # Detect OS and use appropriate sed syntax
    if sed --version >/dev/null 2>&1; then
        # GNU sed (Linux)
        sed -i "$pattern" "$file"
    else
        # BSD sed (macOS)
        sed -i '' "$pattern" "$file"
    fi
}

# Update .ci-operator.yaml with Go and OCP versions
update_ci_operator_yaml() {
    local go_version="$1"
    local ocp_version="$2"

    echo "Updating .ci-operator.yaml..." >&2

    sed_inplace "s/\(tag:\s\+rhel-9-golang-\)\([0-9]\+\.[0-9]\+\)\(\(-builder-multi\)\?-openshift-\)\([0-9]\+\.[0-9]\+\)/\1${go_version}\3${ocp_version}/" .ci-operator.yaml

    echo "  ✅ Updated .ci-operator.yaml" >&2
}

# Update .tekton YAML files with Go version
update_tekton_files() {
    local go_version="$1"

    echo "Updating .tekton/*.yaml files..." >&2

    if [[ -d .tekton ]]; then
        # Update each .tekton YAML file using portable sed
        while IFS= read -r -d '' file; do
            sed_inplace "s/\(brew\.registry\.redhat\.io\/rh-osbs\/openshift-golang-builder:rhel_9_\)\([0-9]\+\.[0-9]\+\)/\1${go_version}/" "$file"
        done < <(find .tekton -name '*.yaml' -type f -print0)
        echo "  ✅ Updated .tekton files" >&2
    else
        echo "  ℹ️  No .tekton directory found, skipping" >&2
    fi
}

# Update Dockerfile with Go version
update_dockerfile() {
    local go_version="$1"

    echo "Updating Dockerfile..." >&2

    sed_inplace "s/\(FROM\s\+golang:\)\([0-9]\+\.[0-9]\+\)/\1${go_version}/" Dockerfile

    echo "  ✅ Updated Dockerfile" >&2
}

# Update Makefile BUILD_IMAGE with Go and OCP versions
update_makefile_build_image() {
    local go_version="$1"
    local ocp_version="$2"

    echo "Updating Makefile BUILD_IMAGE..." >&2

    sed_inplace "s/\(BUILD_IMAGE\s*?=\s*registry\.ci\.openshift\.org\/ocp\/builder:rhel-9-golang-\)\([0-9]\+\.[0-9]\+\)\(\(-builder-multi\)\?-openshift-\)\([0-9]\+\.[0-9]\+\)/\1${go_version}\3${ocp_version}/" Makefile

    echo "  ✅ Updated Makefile BUILD_IMAGE" >&2
}

# Update bundle.konflux.Dockerfile with Go version
update_bundle_konflux_dockerfile() {
    local go_version="$1"

    echo "Updating bundle.konflux.Dockerfile..." >&2

    if [[ -f bundle.konflux.Dockerfile ]]; then
        sed_inplace "s/\(FROM\s\+brew\.registry\.redhat\.io\/rh-osbs\/openshift-golang-builder:rhel_9_\)\([0-9]\+\.[0-9]\+\)/\1${go_version}/" bundle.konflux.Dockerfile
        echo "  ✅ Updated bundle.konflux.Dockerfile" >&2
    else
        echo "  ℹ️  bundle.konflux.Dockerfile not found, skipping" >&2
    fi
}

# Update konflux.Dockerfile with Go version
update_konflux_dockerfile() {
    local go_version="$1"

    echo "Updating konflux.Dockerfile..." >&2

    if [[ -f konflux.Dockerfile ]]; then
        sed_inplace "s/\(FROM\s\+brew\.registry\.redhat\.io\/rh-osbs\/openshift-golang-builder:rhel_9_\)\([0-9]\+\.[0-9]\+\)/\1${go_version}/" konflux.Dockerfile
        echo "  ✅ Updated konflux.Dockerfile" >&2
    else
        echo "  ℹ️  konflux.Dockerfile not found, skipping" >&2
    fi
}

# Update go.mod go directive
update_go_mod_directive() {
    local go_version="$1"

    # go.mod supports either X.Y or X.Y.Z format
    # Use full version if it has a patch, otherwise use X.Y
    local go_mod_version
    if [[ "$go_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        # Has patch version (e.g., 1.24.11)
        go_mod_version="$go_version"
    else
        # Only major.minor (e.g., 1.24)
        go_mod_version="$go_version"
    fi

    echo "Updating go.mod go directive..." >&2

    # Match and replace any go version (X.Y or X.Y.Z or X.Y.Z.W)
    sed_inplace "s/^go [0-9]\+\.[0-9]\+\(\.[0-9]\+\)*$/go ${go_mod_version}/" go.mod

    echo "  ✅ Updated go directive to $go_mod_version" >&2
}

# Update Makefile tool versions
update_makefile_tool_versions() {
    local kustomize_version="$1"
    local controller_tools_version="$2"
    local setup_envtest_version="$3"
    local envtest_k8s_version="$4"
    local golint_version="$5"

    echo "Updating Makefile tool versions..." >&2

    sed_inplace "s/\(KUSTOMIZE_VERSION\s*?=\s*\)v[0-9]\+\.[0-9]\+\.[0-9]\+/\1${kustomize_version}/" Makefile
    sed_inplace "s/\(CONTROLLER_TOOLS_VERSION\s*?=\s*\)v[0-9]\+\.[0-9]\+\.[0-9]\+/\1${controller_tools_version}/" Makefile
    sed_inplace "s/\(SETUP_ENVTEST_VERSION\s*?=\s*\)release-[0-9]\+\.[0-9]\+/\1${setup_envtest_version}/" Makefile
    sed_inplace "s/\(ENVTEST_K8S_VERSION\s*=\s*\)[0-9v]\+\.[0-9]\+\.[0-9]\+/\1${envtest_k8s_version}/" Makefile
    sed_inplace "s/\(GOLINT_VERSION\s*=\s*\)v[0-9]\+\.[0-9]\+\.[0-9]\+/\1${golint_version}/" Makefile

    echo "  ✅ KUSTOMIZE_VERSION = $kustomize_version" >&2
    echo "  ✅ CONTROLLER_TOOLS_VERSION = $controller_tools_version" >&2
    echo "  ✅ SETUP_ENVTEST_VERSION = $setup_envtest_version" >&2
    echo "  ✅ ENVTEST_K8S_VERSION = $envtest_k8s_version" >&2
    echo "  ✅ GOLINT_VERSION = $golint_version" >&2
}
